// SPDX-FileCopyrightText: 2026 Stefan Majewsky <majewsky@gmx.net>
// SPDX-License-Identifier: Apache-2.0

package oblast

import (
	"cmp"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"
)

// plan holds all information that we can derive from reflecting on a given type.
// The queries held within are only valid within the context of a given SQL dialect.
type plan struct {
	TypeName              string   // for use in error messages
	TableName             string   // from info.TableNameIs marker (if any)
	AllColumnNames        []string // in order of struct fields
	PrimaryKeyColumnNames []string // from info.PrimaryKeyIs marker (if any)
	AutoColumnNames       []string // subset of AllColumnNames where field has `,auto` marker

	// Field index (i.e. argument for reflect.Value.FieldByIndex()) for each column name.
	IndexByColumnName map[string][]int
	// Indexes of pointer-typed fields that need to be initialized before scanning into this type.
	IndexesOfTransparentPointerStructs [][]int

	// Planned queries.
	Select plannedQuery // only `SELECT ... FROM ... WHERE `; user supplies the rest during Select{,One}Where()
	Insert plannedQuery
	Upsert plannedQuery
	Update plannedQuery
	Delete plannedQuery
}

// plannedQuery appears in type plan.
type plannedQuery struct {
	// Empty if the respective query type is not supported by this plan for lack of the required marker types.
	Query string
	// Arguments for reflect.Value.FieldByIndex() in the correct order for the query arguments of the above query.
	ArgumentIndexes [][]int
	// Arguments for reflect.Value.FieldByIndex() in the correct order for the Scan() arguments of the above query.
	ScanIndexes [][]int
}

// planOpts holds additional arguments to buildPlan().
type planOpts struct {
	StructTagKey          string // defaults to "db"
	TableName             string
	PrimaryKeyColumnNames []string
}

// buildPlan creates a new plan for the given struct type.
func buildPlan(t reflect.Type, dialect Dialect, opts planOpts) (plan, error) {
	if t.Kind() != reflect.Struct {
		return plan{}, fmt.Errorf("expected struct type, but got kind %q", t.Kind().String())
	}

	// apply defaults to planOpts fields
	if opts.StructTagKey == "" {
		opts.StructTagKey = "db"
	}

	var p = plan{
		TypeName:              t.Name(),
		TableName:             opts.TableName,
		PrimaryKeyColumnNames: opts.PrimaryKeyColumnNames,
		IndexByColumnName:     make(map[string][]int),
	}

	var (
		indexesOfOpaqueStructs            [][]int
		indexesOfUnusedTransparentStructs [][]int
	)
	isWithin := func(fieldIndex, structIndex []int) bool {
		// returns whether `structIndex` is a prefix of `fieldIndex` (i.e. whether the field is contained within the struct)
		return len(fieldIndex) > len(structIndex) && slices.Equal(fieldIndex[0:len(structIndex)], structIndex)
	}

	// discover addressable fields in this type, collect information from markers and tags
	for _, field := range reflect.VisibleFields(t) {
		// ignore unexported fields (otherwise reflect.Value.Interface() on the field would panic)
		if field.PkgPath != "" {
			continue
		}

		// recurse into struct fields (i.e. ignore the struct itself and consider its members instead)
		// unless the field itself has a `db:"..."` tag
		if field.Type.Kind() == reflect.Struct || (field.Type.Kind() == reflect.Pointer && field.Type.Elem().Kind() == reflect.Struct) {
			if field.Tag.Get(opts.StructTagKey) == "" {
				indexesOfUnusedTransparentStructs = append(indexesOfUnusedTransparentStructs, field.Index)
				if field.Type.Kind() == reflect.Pointer {
					// remember that, when scanning into a record of type `t`, we need to write a non-nil zeroed struct into this field
					// to enable taking an address of its mapped member fields
					p.IndexesOfTransparentPointerStructs = append(p.IndexesOfTransparentPointerStructs, field.Index)
				}
				continue
			}
			indexesOfOpaqueStructs = append(indexesOfOpaqueStructs, field.Index)
		}

		// ignore fields that are within a struct type that is mapped as a whole
		if slices.ContainsFunc(indexesOfOpaqueStructs, func(index []int) bool {
			return isWithin(field.Index, index)
		}) {
			continue
		}

		// check `db:"..."` tag, ignore fields that are declared with column name "-"
		tags := strings.Split(strings.TrimSpace(field.Tag.Get(opts.StructTagKey)), ",")
		columnName, extraTags := cmp.Or(tags[0], field.Name), tags[1:]
		if columnName == "-" {
			continue
		}

		if otherIndex := p.IndexByColumnName[columnName]; otherIndex != nil {
			return plan{}, fmt.Errorf(
				"duplicate tag `%s:%q` on field index %v, but also on field index %v",
				opts.StructTagKey, columnName, otherIndex, field.Index,
			)
		}
		p.IndexByColumnName[columnName] = field.Index
		p.AllColumnNames = append(p.AllColumnNames, columnName)

		// track whether transparent structs contain fields that are mapped
	restartIteration:
		for idx, index := range indexesOfUnusedTransparentStructs {
			if isWithin(field.Index, index) {
				indexesOfUnusedTransparentStructs = slices.Delete(indexesOfUnusedTransparentStructs, idx, idx+1)
				goto restartIteration
			}
		}

		for _, tag := range extraTags {
			switch tag {
			case "auto":
				p.AutoColumnNames = append(p.AutoColumnNames, columnName)
			default:
				return plan{}, fmt.Errorf("unknown option `%s:%q` on field %q", opts.StructTagKey, ","+tag, field.Name)
			}
		}
	}

	// validation: transparent structs need to have at least one of their members mapped
	// (this property is most often violated when a user of a library-defined type is not aware that this type is a struct under the hood,
	// e.g. a field like "CreatedAt time.Time" needs to have a tag like `db:"created_at"`,
	// otherwise nothing will be mapped because time.Time does not have any exported fields)
	for _, index := range indexesOfUnusedTransparentStructs {
		field := t.FieldByIndex(index)
		return plan{}, fmt.Errorf(
			"field %q of type %s does not contain any mapped fields (to map this whole field to a DB column, add an explicit `%s:\"...\"` tag)",
			field.Name, field.Type.String(), opts.StructTagKey,
		)
	}

	// validation: defining a primary key only makes sense for records that map onto a single table
	if len(p.PrimaryKeyColumnNames) > 0 && p.TableName == "" {
		return plan{}, errors.New("cannot declare a primary key without also providing the TableNameIs option")
	}

	// validation: oblast.PrimaryKeyInfo must refer to columns that exist
	for _, columnName := range p.PrimaryKeyColumnNames {
		_, ok := p.IndexByColumnName[columnName]
		if !ok {
			return plan{}, fmt.Errorf("no field has tag `%s:%q`, but a field of this name was declared in the primary key", opts.StructTagKey, columnName)
		}
	}

	// prepare query strings
	p.Select = p.buildSelectQueryIfPossible(dialect)
	p.Insert = p.buildInsertQueryIfPossible(dialect, false)
	p.Upsert = p.buildInsertQueryIfPossible(dialect, true)
	p.Update = p.buildUpdateQueryIfPossible(dialect)
	p.Delete = p.buildDeleteQueryIfPossible(dialect)

	return p, nil
}

func (p plan) getNonAutoColumnNames() []string {
	result := make([]string, 0, len(p.AllColumnNames)-len(p.AutoColumnNames))
	for _, columnName := range p.AllColumnNames {
		if !slices.Contains(p.AutoColumnNames, columnName) {
			result = append(result, columnName)
		}
	}
	return result
}

func (p plan) getNonPrimaryKeyColumnNames() []string {
	result := make([]string, 0, len(p.AllColumnNames)-len(p.PrimaryKeyColumnNames))
	for _, columnName := range p.AllColumnNames {
		if !slices.Contains(p.PrimaryKeyColumnNames, columnName) {
			result = append(result, columnName)
		}
	}
	return result
}

func (p plan) buildSelectQueryIfPossible(dialect Dialect) plannedQuery {
	if p.TableName == "" {
		return plannedQuery{Query: ""}
	}

	var (
		scanIndexes       = make([][]int, len(p.AllColumnNames))
		quotedColumnNames = make([]string, len(p.AllColumnNames))
	)
	for idx, columnName := range p.AllColumnNames {
		scanIndexes[idx] = p.IndexByColumnName[columnName]
		quotedColumnNames[idx] = dialect.QuoteIdentifier(columnName)
	}

	query := fmt.Sprintf(
		`SELECT %s FROM %s WHERE `,
		strings.Join(quotedColumnNames, ", "),
		dialect.QuoteIdentifier(p.TableName),
	)
	return plannedQuery{query, nil, scanIndexes}
}

func (p plan) buildInsertQueryIfPossible(dialect Dialect, isUpsert bool) plannedQuery {
	if p.TableName == "" || len(p.AllColumnNames) == 0 {
		return plannedQuery{Query: ""}
	}
	nonAutoColumnNames := p.getNonAutoColumnNames()
	if len(nonAutoColumnNames) == 0 {
		return plannedQuery{Query: ""}
	}

	// UPSERT queries specifically are only generated if we have non-auto primary keys:
	// - cannot hit a key conflict if there are no keys
	// - cannot hit a key conflict on insert if all keys are autogenerated (and thus we never supply them during INSERT)
	if isUpsert && !slices.ContainsFunc(p.PrimaryKeyColumnNames, func(n string) bool { return !slices.Contains(p.AutoColumnNames, n) }) {
		return plannedQuery{Query: ""}
	}

	var (
		argumentIndexes    = make([][]int, len(nonAutoColumnNames))
		scanIndexes        [][]int
		quotedColumnNames  = make([]string, len(nonAutoColumnNames))
		quotedPlaceholders = make([]string, len(nonAutoColumnNames))
	)
	for idx, columnName := range nonAutoColumnNames {
		argumentIndexes[idx] = p.IndexByColumnName[columnName]
		quotedColumnNames[idx] = dialect.QuoteIdentifier(columnName)
		quotedPlaceholders[idx] = dialect.Placeholder(idx)
	}
	if len(p.AutoColumnNames) > 0 {
		scanIndexes = make([][]int, len(p.AutoColumnNames))
		for idx, columnName := range p.AutoColumnNames {
			scanIndexes[idx] = p.IndexByColumnName[columnName]
		}
	}

	query := fmt.Sprintf(
		`INSERT INTO %s (%s) VALUES (%s)`,
		dialect.QuoteIdentifier(p.TableName),
		strings.Join(quotedColumnNames, ", "),
		strings.Join(quotedPlaceholders, ", "),
	)
	if isUpsert {
		query += dialect.UpsertClause(p.PrimaryKeyColumnNames, p.getNonPrimaryKeyColumnNames())
	}
	if len(p.AutoColumnNames) > 0 {
		quotedAutoColumns := make([]string, len(p.AutoColumnNames))
		for idx, name := range p.AutoColumnNames {
			quotedAutoColumns[idx] = dialect.QuoteIdentifier(name)
		}
		query += ` RETURNING ` + strings.Join(quotedAutoColumns, ", ")
	}
	return plannedQuery{query, argumentIndexes, scanIndexes}
}

func (p plan) buildUpdateQueryIfPossible(dialect Dialect) plannedQuery {
	if p.TableName == "" || len(p.PrimaryKeyColumnNames) == 0 {
		return plannedQuery{Query: ""}
	}
	nonPrimaryKeyColumnNames := p.getNonPrimaryKeyColumnNames()
	if len(nonPrimaryKeyColumnNames) == 0 {
		return plannedQuery{Query: ""}
	}

	var (
		setArgumentIndexes = make([][]int, len(nonPrimaryKeyColumnNames))
		setClauses         = make([]string, len(nonPrimaryKeyColumnNames))
	)
	for idx, columnName := range nonPrimaryKeyColumnNames {
		setArgumentIndexes[idx] = p.IndexByColumnName[columnName]
		setClauses[idx] = fmt.Sprintf("%s = %s", dialect.QuoteIdentifier(columnName), dialect.Placeholder(idx))
	}

	var (
		whereArgumentIndexes = make([][]int, len(p.PrimaryKeyColumnNames))
		whereClauses         = make([]string, len(p.PrimaryKeyColumnNames))
	)
	for idx, columnName := range p.PrimaryKeyColumnNames {
		whereArgumentIndexes[idx] = p.IndexByColumnName[columnName]
		whereClauses[idx] = fmt.Sprintf("%s = %s", dialect.QuoteIdentifier(columnName), dialect.Placeholder(idx+len(setClauses)))
	}

	query := fmt.Sprintf(
		`UPDATE %s SET %s WHERE %s`,
		dialect.QuoteIdentifier(p.TableName),
		strings.Join(setClauses, ", "),
		strings.Join(whereClauses, " AND "),
	)
	return plannedQuery{query, slices.Concat(setArgumentIndexes, whereArgumentIndexes), nil}
}

func (p plan) buildDeleteQueryIfPossible(dialect Dialect) plannedQuery {
	if p.TableName == "" || len(p.PrimaryKeyColumnNames) == 0 {
		return plannedQuery{Query: ""}
	}

	var (
		argumentIndexes = make([][]int, len(p.PrimaryKeyColumnNames))
		clauses         = make([]string, len(p.PrimaryKeyColumnNames))
	)
	for idx, columnName := range p.PrimaryKeyColumnNames {
		argumentIndexes[idx] = p.IndexByColumnName[columnName]
		clauses[idx] = fmt.Sprintf("%s = %s", dialect.QuoteIdentifier(columnName), dialect.Placeholder(idx))
	}

	query := fmt.Sprintf(
		`DELETE FROM %s WHERE %s`,
		dialect.QuoteIdentifier(p.TableName),
		strings.Join(clauses, " AND "),
	)
	return plannedQuery{query, argumentIndexes, nil}
}
