package policy

import (
	"encoding/json"
	"fmt"
	"maps"
	"strings"

	pgquery "github.com/pganalyze/pg_query_go/v6"
)

var statements = set(`ReturnStmt SelectStmt InsertStmt UpdateStmt DeleteStmt MergeStmt CreateStmt AlterTableStmt IndexStmt ViewStmt CreateTableAsStmt RefreshMatViewStmt CreateSeqStmt AlterSeqStmt CompositeTypeStmt CreateEnumStmt AlterEnumStmt CreateDomainStmt AlterDomainStmt CreateFunctionStmt AlterFunctionStmt CreateTrigStmt CreatePolicyStmt AlterPolicyStmt DropStmt TruncateStmt CommentStmt ExplainStmt RenameStmt CallStmt`)

var objects = set(`OBJECT_TABLE OBJECT_INDEX OBJECT_VIEW OBJECT_MATVIEW OBJECT_SEQUENCE OBJECT_TYPE OBJECT_DOMAIN OBJECT_FUNCTION OBJECT_PROCEDURE OBJECT_ROUTINE OBJECT_TRIGGER OBJECT_POLICY OBJECT_COLUMN OBJECT_TABCONSTRAINT OBJECT_ATTRIBUTE`)

var alterations = set(`AT_AddColumn AT_AddColumnToView AT_ColumnDefault AT_CookedColumnDefault AT_DropNotNull AT_SetNotNull AT_SetStatistics AT_SetOptions AT_ResetOptions AT_SetStorage AT_SetCompression AT_DropColumn AT_AddIndex AT_ReAddIndex AT_AddConstraint AT_ReAddConstraint AT_ReAddDomainConstraint AT_AlterConstraint AT_ValidateConstraint AT_DropConstraint AT_AlterColumnType AT_AlterColumnGenericOptions AT_ChangePersistence AT_SetRelOptions AT_ResetRelOptions AT_ReplaceRelOptions AT_EnableTrig AT_EnableAlwaysTrig AT_EnableReplicaTrig AT_DisableTrig AT_EnableTrigAll AT_DisableTrigAll AT_EnableTrigUser AT_DisableTrigUser AT_AddInherit AT_DropInherit AT_AddOf AT_DropOf AT_ReplicaIdentity AT_EnableRowSecurity AT_DisableRowSecurity AT_ForceRowSecurity AT_NoForceRowSecurity AT_GenericOptions AT_AttachPartition AT_DetachPartition AT_AddIdentity AT_SetIdentity AT_DropIdentity AT_SetExpression AT_DropExpression AT_SetLogged AT_SetUnLogged`)

var builtins = set(`abs acos acosh asin asinh atan atan2 atanh ceil ceiling cbrt cos cosh cot degrees div exp factorial floor gcd greatest lcm least ln log log10 mod pi power radians round scale sign sin sinh sqrt tan tanh trim_scale trunc width_bucket random setseed min max sum avg count every bool_and bool_or bit_and bit_or bit_xor array_agg string_agg json_agg jsonb_agg json_object_agg jsonb_object_agg stddev stddev_pop stddev_samp variance var_pop var_samp percentile_cont percentile_disc mode rank dense_rank row_number percent_rank cume_dist ntile lag lead first_value last_value nth_value lower upper length char_length character_length octet_length bit_length concat concat_ws format left right lpad rpad ltrim rtrim btrim trim substring substr overlay position strpos replace reverse repeat split_part starts_with ascii chr initcap translate md5 sha224 sha256 sha384 sha512 encode decode quote_ident quote_literal quote_nullable regexp_count regexp_instr regexp_like regexp_match regexp_matches regexp_replace regexp_split_to_array regexp_split_to_table regexp_substr to_ascii to_hex to_bin normalize is_normalized to_char to_date to_number to_timestamp date_part date_trunc date_bin age make_date make_time make_timestamp make_timestamptz make_interval extract now transaction_timestamp statement_timestamp clock_timestamp timeofday isfinite justify_days justify_hours justify_interval timezone overlaps generate_series generate_subscripts unnest array_append array_prepend array_cat array_dims array_fill array_length array_lower array_upper array_ndims array_position array_positions array_remove array_replace array_to_string cardinality string_to_array string_to_table trim_array array_sample array_shuffle json_build_array jsonb_build_array json_build_object jsonb_build_object json_object jsonb_object to_json to_jsonb row_to_json array_to_json json_array_length jsonb_array_length json_each jsonb_each json_each_text jsonb_each_text json_extract_path jsonb_extract_path json_extract_path_text jsonb_extract_path_text json_object_keys jsonb_object_keys json_populate_record jsonb_populate_record json_populate_recordset jsonb_populate_recordset json_to_record jsonb_to_record json_to_recordset jsonb_to_recordset json_array_elements jsonb_array_elements json_array_elements_text jsonb_array_elements_text json_strip_nulls jsonb_strip_nulls json_typeof jsonb_typeof jsonb_set jsonb_set_lax jsonb_insert jsonb_pretty jsonb_path_exists jsonb_path_match jsonb_path_query jsonb_path_query_array jsonb_path_query_first jsonb_path_exists_tz jsonb_path_match_tz jsonb_path_query_tz jsonb_path_query_array_tz jsonb_path_query_first_tz nextval currval lastval setval gen_random_uuid uuidv4 uuidv7 uuid_extract_timestamp uuid_extract_version pg_typeof pg_column_size pg_size_pretty pg_backend_pid pg_sleep pg_sleep_for pg_sleep_until current_database current_schema current_schemas current_setting version obj_description col_description has_schema_privilege has_table_privilege has_function_privilege has_database_privilege has_sequence_privilege has_type_privilege has_column_privilege has_any_column_privilege to_regclass to_regtype to_regproc to_regprocedure to_regnamespace to_regrole pg_get_serial_sequence pg_get_expr pg_get_viewdef pg_get_constraintdef pg_get_indexdef pg_get_functiondef pg_get_function_arguments pg_get_function_result pg_table_size pg_relation_size pg_total_relation_size pg_indexes_size pg_is_in_recovery inet_client_addr inet_client_port inet_server_addr inet_server_port`)

func set(s string) map[string]bool {
	m := map[string]bool{}

	for v := range strings.FieldsSeq(s) {
		m[v] = true
	}

	return m
}

type checker struct {
	schema string
	local  map[string]bool
	depth  int
}

func Check(sql, schema string, local map[string]bool) error {
	copyLocal := map[string]bool{}

	maps.Copy(copyLocal, local)

	c := checker{schema: schema, local: copyLocal}
	return c.parse(sql, true)
}

func (c checker) parse(sql string, single bool) error {
	if c.depth > 16 {
		return fmt.Errorf("function nesting exceeds 16")
	}

	raw, err := pgquery.ParseToJSON(sql)
	if err != nil {
		return fmt.Errorf("parse SQL: %w", err)
	}

	var tree map[string]any

	if err = json.Unmarshal([]byte(raw), &tree); err != nil {
		return err
	}

	list, _ := tree["stmts"].([]any)

	if len(list) == 0 || (single && len(list) != 1) {
		return fmt.Errorf("expected %s", map[bool]string{true: "one statement", false: "a nonempty SQL body"}[single])
	}

	return c.walk(tree)
}

func (c checker) walk(v any) error {
	switch x := v.(type) {
	case []any:
		for _, v := range x {
			if err := c.walk(v); err != nil {
				return err
			}
		}
	case map[string]any:
		for key, value := range x {
			if len(key) > 0 && key[0] >= 'A' && key[0] <= 'Z' && strings.HasSuffix(key, "Stmt") && !statements[key] {
				return fmt.Errorf("statement %s is not allowed", key)
			}

			node, _ := value.(map[string]any)

			switch key {
			case "DropStmt":
				if !objects[str(node["removeType"])] {
					return fmt.Errorf("object kind is not local")
				}
			case "RenameStmt", "CommentStmt":
				field := "renameType"
				if key == "CommentStmt" {
					field = "objtype"
				}
				if !objects[str(node[field])] {
					return fmt.Errorf("object kind is not local")
				}
			case "AlterTableStmt":
				if !objects[str(node["objtype"])] {
					return fmt.Errorf("object kind is not local")
				}
			case "AlterTableCmd":
				if !alterations[str(node["subtype"])] {
					return fmt.Errorf("table alteration %s is not allowed", str(node["subtype"]))
				}
			case "RangeVar":
				if node["relpersistence"] == "t" || node["relpersistence"] == "u" {
					return fmt.Errorf("temporary and unlogged relations are not supported")
				}
			case "IndexStmt":
				if node["concurrent"] == true || str(node["tableSpace"]) != "" {
					return fmt.Errorf("concurrent indexes and tablespaces are not supported")
				}
			case "FuncCall":
				if err := c.function(names(node["funcname"])); err != nil {
					return err
				}
			case "CreateTrigStmt":
				if err := c.function(names(node["funcname"])); err != nil {
					return err
				}
			case "CreateFunctionStmt":
				if err := c.definition(node); err != nil {
					return err
				}
			case "DefElem":
				if str(node["defname"]) == "set" || str(node["defname"]) == "support" {
					return fmt.Errorf("function settings and support hooks are not allowed")
				}
			}

			if key == "funcname" {
				if err := c.function(names(value)); err != nil {
					return err
				}
			}

			if key == "relpersistence" && (value == "t" || value == "u") {
				return fmt.Errorf("temporary and unlogged relations are not supported")
			}

			if key == "tablespacename" && str(value) != "" {
				return fmt.Errorf("tablespaces are not supported")
			}

			if err := c.walk(value); err != nil {
				return err
			}
		}
	}

	return nil
}

func (c checker) function(parts []string) error {
	if len(parts) == 0 || len(parts) > 2 {
		return fmt.Errorf("invalid function name")
	}

	name := parts[len(parts)-1]
	namespace := ""

	if len(parts) == 2 {
		namespace = parts[0]
	}

	if namespace != "" && namespace != "pg_catalog" && namespace != c.schema {
		return fmt.Errorf("external function namespace is not allowed")
	}

	if namespace != "pg_catalog" && c.local[name] {
		return nil
	}

	if namespace != c.schema && builtins[name] {
		return nil
	}

	return fmt.Errorf("function %s is not in the SQL policy", strings.Join(parts, "."))
}

func (c checker) definition(node map[string]any) error {
	parts := names(node["funcname"])

	if len(parts) == 0 || len(parts) > 2 || (len(parts) == 2 && parts[0] != c.schema) {
		return fmt.Errorf("function must be islet-local")
	}

	name := parts[len(parts)-1]

	if allowed, exists := c.local[name]; exists && !allowed {
		return fmt.Errorf("function name conflicts with a catalog routine")
	}

	c.local[name] = true
	opts, _ := node["options"].([]any)
	lang := ""
	body := ""

	for _, option := range opts {
		o, _ := option.(map[string]any)
		d, _ := o["DefElem"].(map[string]any)

		switch str(d["defname"]) {
		case "language":
			lang = stringNode(d["arg"])
		case "as":
			a, _ := d["arg"].(map[string]any)
			l, _ := a["List"].(map[string]any)
			items, _ := l["items"].([]any)
			if len(items) != 1 {
				return fmt.Errorf("expected a SQL function body")
			}
			body = stringNode(items[0])
		case "set", "support":
			return fmt.Errorf("function configuration and support hooks are not allowed")
		}
	}

	if lang != "sql" {
		return fmt.Errorf("only SQL language functions and procedures are supported")
	}

	if body != "" {
		c.depth++
		return c.parse(body, false)
	}

	if node["sql_body"] == nil {
		return fmt.Errorf("SQL function body is required")
	}

	return nil
}

func str(v any) string {
	s, _ := v.(string)
	return s
}

func stringNode(v any) string {
	n, _ := v.(map[string]any)
	s, _ := n["String"].(map[string]any)
	return str(s["sval"])
}

func names(v any) []string {
	a, _ := v.([]any)
	out := make([]string, 0, len(a))

	for _, n := range a {
		out = append(out, stringNode(n))
	}

	return out
}
