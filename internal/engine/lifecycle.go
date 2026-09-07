package engine

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

func (m *Manager) Create(ctx context.Context) (Islet, error) {
	return m.create(ctx, false)
}

func (m *Manager) create(ctx context.Context, initializing bool) (Islet, error) {
	ctx, cancel := context.WithTimeout(ctx, m.config.OperationTimeout)
	defer cancel()

	id, err := opaque()
	if err != nil {
		return Islet{}, err
	}

	password, err := opaque()
	if err != nil {
		return Islet{}, err
	}

	token := ""
	state := "active"

	if initializing {
		state = "initializing"
		token, err = opaque()
		if err != nil {
			return Islet{}, err
		}
	}

	md := metadata{Islet: Islet{ID: id, generation: 1, State: state}, schema: "pgislet_i_" + id, role: "pgislet_r_" + id, password: password, token: token}
	tx, err := m.pool.Begin(ctx)
	if err != nil {
		return Islet{}, wrap(ErrLifecycle, err)
	}

	defer rollback(tx)

	sql := `CREATE ROLE ` + ident(md.role) + ` LOGIN PASSWORD ` + literal(password) + ` NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT NOREPLICATION NOBYPASSRLS;
 ALTER ROLE ` + ident(md.role) + ` SET search_path TO ` + ident(md.schema) + `,pg_catalog;
 ALTER ROLE ` + ident(md.role) + ` SET statement_timeout TO ` + literal(fmt.Sprint(m.config.StatementTimeout.Milliseconds())) + `;
 ALTER ROLE ` + ident(md.role) + ` SET lock_timeout TO ` + literal(fmt.Sprint(m.config.LockTimeout.Milliseconds())) + `;
 ALTER ROLE ` + ident(md.role) + ` SET idle_in_transaction_session_timeout TO ` + literal(fmt.Sprint(m.config.IdleTransactionTimeout.Milliseconds())) + `;`

	if _, err = tx.Exec(ctx, sql); err != nil {
		return Islet{}, wrap(ErrLifecycle, err)
	}

	if err = m.createSchema(ctx, tx, md); err != nil {
		return Islet{}, wrap(ErrLifecycle, err)
	}

	if _, err = tx.Exec(ctx, `GRANT CONNECT ON DATABASE `+ident(m.runtime.Database)+` TO `+ident(md.role)+`;
 GRANT USAGE ON SCHEMA pgislet_api TO `+ident(md.role)+`;
 GRANT EXECUTE ON FUNCTION pgislet_api.enter(text,bigint,text),pgislet_api.finish(text,bigint,text) TO `+ident(md.role)); err != nil {
		return Islet{}, wrap(ErrLifecycle, err)
	}

	err = tx.QueryRow(ctx, `INSERT INTO pgislet_internal.islets(id,schema_name,role_name,password,state,generation,init_token) VALUES($1,$2,$3,$4,$5,1,NULLIF($6,'')) RETURNING created_at,updated_at`, id, md.schema, md.role, password, state, token).Scan(&md.CreatedAt, &md.UpdatedAt)
	if err != nil {
		return Islet{}, wrap(ErrLifecycle, err)
	}

	return md.Islet, commit(ctx, tx)
}

func (m *Manager) createSchema(ctx context.Context, tx pgx.Tx, md metadata) error {
	_, err := tx.Exec(ctx, `CREATE SCHEMA `+ident(md.schema)+`; REVOKE ALL ON SCHEMA `+ident(md.schema)+` FROM PUBLIC; GRANT USAGE,CREATE ON SCHEMA `+ident(md.schema)+` TO `+ident(md.role))
	return err
}

func lockMetadata(ctx context.Context, tx pgx.Tx, h Islet) (metadata, error) {
	md, err := readMetadata(tx.QueryRow(ctx, metadataSQL+` WHERE id=$1 FOR UPDATE NOWAIT`, h.ID))
	if err != nil {
		return md, err
	}

	if md.generation != h.generation {
		return md, ErrStaleGeneration
	}

	return md, nil
}

func (m *Manager) Reset(ctx context.Context, h Islet) (Islet, error) {
	return m.reset(ctx, h, false, false)
}

func (m *Manager) Recover(ctx context.Context, h Islet) (Islet, error) {
	return m.reset(ctx, h, false, true)
}

func (m *Manager) reset(ctx context.Context, h Islet, initialize, recovering bool) (Islet, error) {
	ctx, cancel := context.WithTimeout(ctx, m.config.OperationTimeout)
	defer cancel()

	tx, err := m.pool.Begin(ctx)
	if err != nil {
		return Islet{}, wrap(ErrLifecycle, err)
	}

	defer rollback(tx)

	md, err := lockMetadata(ctx, tx, h)
	if err != nil {
		return Islet{}, err
	}

	if md.State == "initializing" && !recovering {
		return Islet{}, ErrUnavailable
	}

	if err = checkDependencies(ctx, tx, md.schema); err != nil {
		return Islet{}, m.failLifecycle(ctx, tx, md, err)
	}

	if _, err = tx.Exec(ctx, `SAVEPOINT cleanup`); err != nil {
		return Islet{}, err
	}

	if _, err = tx.Exec(ctx, `DROP SCHEMA `+ident(md.schema)+` CASCADE`); err == nil {
		err = m.createSchema(ctx, tx, md)
	}

	if err != nil {
		_, _ = tx.Exec(ctx, `ROLLBACK TO SAVEPOINT cleanup`)
		return Islet{}, m.failLifecycle(ctx, tx, md, err)
	}

	state, token := "active", ""

	if initialize {
		state = "initializing"
		token, err = opaque()
		if err != nil {
			return Islet{}, err
		}
	}

	err = tx.QueryRow(ctx, `UPDATE pgislet_internal.islets SET state=$2,generation=generation+1,init_token=NULLIF($3,''),updated_at=clock_timestamp() WHERE id=$1 RETURNING generation,state,updated_at`, h.ID, state, token).Scan(&md.generation, &md.State, &md.UpdatedAt)
	if err != nil {
		return Islet{}, wrap(ErrLifecycle, err)
	}

	return md.Islet, commit(ctx, tx)
}

func (m *Manager) failLifecycle(ctx context.Context, tx pgx.Tx, md metadata, cause error) error {
	if _, err := tx.Exec(ctx, `UPDATE pgislet_internal.islets SET state='failed',init_token=NULL,updated_at=clock_timestamp() WHERE id=$1`, md.ID); err != nil {
		return wrap(ErrLifecycle, errors.Join(cause, err))
	}

	return wrap(ErrLifecycle, errors.Join(cause, commit(ctx, tx)))
}

func (m *Manager) Delete(ctx context.Context, h Islet) error {
	ctx, cancel := context.WithTimeout(ctx, m.config.OperationTimeout)
	defer cancel()

	tx, err := m.pool.Begin(ctx)
	if err != nil {
		return wrap(ErrLifecycle, err)
	}

	defer rollback(tx)

	md, err := lockMetadata(ctx, tx, h)
	if errors.Is(err, ErrNotFound) {
		return nil
	}

	if err != nil {
		return err
	}

	if md.State == "initializing" {
		return ErrUnavailable
	}

	if err = checkDependencies(ctx, tx, md.schema); err != nil {
		return m.failLifecycle(ctx, tx, md, err)
	}

	if _, err = tx.Exec(ctx, `SAVEPOINT cleanup`); err != nil {
		return err
	}

	_, err = tx.Exec(ctx, `DROP SCHEMA `+ident(md.schema)+` CASCADE;
 REVOKE ALL ON DATABASE `+ident(m.runtime.Database)+` FROM `+ident(md.role)+`;
 REVOKE ALL ON SCHEMA pgislet_api FROM `+ident(md.role)+`;
 REVOKE ALL ON FUNCTION pgislet_api.enter(text,bigint,text),pgislet_api.finish(text,bigint,text) FROM `+ident(md.role)+`;
 DROP ROLE `+ident(md.role))
	if err != nil {
		_, _ = tx.Exec(ctx, `ROLLBACK TO SAVEPOINT cleanup`)
		return m.failLifecycle(ctx, tx, md, err)
	}

	if _, err = tx.Exec(ctx, `DELETE FROM pgislet_internal.islets WHERE id=$1`, h.ID); err != nil {
		return wrap(ErrLifecycle, err)
	}

	return commit(ctx, tx)
}

func (m *Manager) CreateWithInitialization(ctx context.Context, statements []string) (Islet, error) {
	ctx, cancel := context.WithTimeout(ctx, m.config.OperationTimeout)
	defer cancel()

	if err := m.checkSize(statements); err != nil {
		return Islet{}, err
	}

	h, err := m.create(ctx, true)
	if err != nil {
		return h, err
	}

	return m.initialize(ctx, h, statements)
}

func (m *Manager) Reinitialize(ctx context.Context, h Islet, statements []string) (Islet, error) {
	ctx, cancel := context.WithTimeout(ctx, m.config.OperationTimeout)
	defer cancel()

	if err := m.checkSize(statements); err != nil {
		return h, err
	}

	next, err := m.reset(ctx, h, true, false)
	if err != nil {
		return next, err
	}

	return m.initialize(ctx, next, statements)
}

func (m *Manager) initialize(ctx context.Context, h Islet, statements []string) (Islet, error) {
	md, err := m.lookup(ctx, h.ID)
	if err == nil && md.generation != h.generation {
		err = ErrStaleGeneration
	}

	if err == nil {
		_, err = m.execute(ctx, md, statements, true)
	}

	if err != nil {
		recovery, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		_, stateErr := m.pool.Exec(recovery, `UPDATE pgislet_internal.islets SET state='failed',init_token=NULL,updated_at=clock_timestamp() WHERE id=$1 AND generation=$2 AND state='initializing'`, h.ID, h.generation)
		current, openErr := m.Open(recovery, h.ID)

		if openErr == nil {
			h = current
		}

		return h, wrap(ErrInitialization, errors.Join(err, stateErr, openErr))
	}

	h.State = "active"
	current, err := m.Open(ctx, h.ID)
	if err != nil {
		return h, err
	}

	return current, nil
}
