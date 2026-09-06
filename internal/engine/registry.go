package engine

import (
	"context"

	"github.com/jackc/pgx/v5"
)

type metadata struct {
	Islet
	schema   string
	role     string
	password string
	token    string
}

func (m *Manager) lookup(ctx context.Context, id string) (metadata, error) {
	return readMetadata(m.pool.QueryRow(ctx, metadataSQL+" WHERE id=$1", id))
}

const metadataSQL = `SELECT id,generation,state,created_at,updated_at,schema_name,role_name,password,COALESCE(init_token,'') FROM pgislet_internal.islets`

func readMetadata(row pgx.Row) (metadata, error) {
	var md metadata
	err := row.Scan(&md.ID, &md.Generation, &md.State, &md.CreatedAt, &md.UpdatedAt, &md.schema, &md.role, &md.password, &md.token)
	if err == pgx.ErrNoRows {
		return md, ErrNotFound
	}

	return md, classify(err)
}
