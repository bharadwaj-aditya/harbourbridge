// Copyright 2020 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package common

import (
	"fmt"
	"sort"

	"github.com/GoogleCloudPlatform/spanner-migration-tool/internal"
	"github.com/GoogleCloudPlatform/spanner-migration-tool/schema"
	"github.com/GoogleCloudPlatform/spanner-migration-tool/spanner/ddl"
)

type UtilsOrderInterface interface {
	initPrimaryKeyOrder(conv *internal.Conv)
	initIndexOrder(conv *internal.Conv)
}
type UtilsOrderImpl struct{}

// ToNotNull returns true if a column is not nullable and false if it is.
func ToNotNull(conv *internal.Conv, isNullable string) bool {
	switch isNullable {
	case "YES":
		return false
	case "NO":
		return true
	}
	conv.Unexpected(fmt.Sprintf("isNullable column has unknown value: %s", isNullable))
	return false
}

// GetColsAndSchemas provides information about columns and schema for a table.
func GetColsAndSchemas(conv *internal.Conv, tableId string) (schema.Table, string, []string, ddl.CreateTable, error) {
	srcSchema := conv.SrcSchema[tableId]
	spTableName, err1 := internal.GetSpannerTable(conv, tableId)
	srcCols := []string{}
	for _, colId := range srcSchema.ColIds {
		srcCols = append(srcCols, srcSchema.ColDefs[colId].Name)
	}
	spCols, err2 := internal.GetSpannerCols(conv, tableId, srcCols)
	spSchema, ok := conv.SpSchema[tableId]
	var err error
	if err1 != nil || err2 != nil || !ok {
		err = fmt.Errorf(fmt.Sprintf("err1=%s, err2=%s, ok=%t", err1, err2, ok))
	}
	return srcSchema, spTableName, spCols, spSchema, err
}

func GetSortedTableIdsBySrcName(srcSchema map[string]schema.Table) []string {
	tableNameIdMap := map[string]string{}
	var tableNames, sortedTableIds []string
	for id, srcTable := range srcSchema {
		tableNames = append(tableNames, srcTable.Name)
		tableNameIdMap[srcTable.Name] = id
	}
	sort.Strings(tableNames)
	for _, name := range tableNames {
		sortedTableIds = append(sortedTableIds, tableNameIdMap[name])
	}
	return sortedTableIds
}

func GetSortedTableIdsBySpName(spSchema ddl.Schema) []string {
	tableNameIdMap := map[string]string{}
	tableNames := []string{}
	sortedTableIds := []string{}
	for id, spTable := range spSchema {
		tableNames = append(tableNames, spTable.Name)
		tableNameIdMap[spTable.Name] = id
	}
	sort.Strings(tableNames)
	for _, name := range tableNames {
		sortedTableIds = append(sortedTableIds, tableNameIdMap[name])
	}
	return sortedTableIds
}

func (uo *UtilsOrderImpl) initPrimaryKeyOrder(conv *internal.Conv) {
	for k, table := range conv.SrcSchema {
		for i := range table.PrimaryKeys {
			conv.SrcSchema[k].PrimaryKeys[i].Order = i + 1
		}
	}
}

func (uo *UtilsOrderImpl) initIndexOrder(conv *internal.Conv) {
	for k, table := range conv.SrcSchema {
		for i, index := range table.Indexes {
			for j := range index.Keys {
				conv.SrcSchema[k].Indexes[i].Keys[j].Order = j + 1
			}
		}
	}
}

func IntersectionOfTwoStringSlices(a []string, b []string) []string {
	set := make([]string, 0)
	hash := make(map[string]struct{})

	for _, v := range a {
		hash[v] = struct{}{}
	}

	for _, v := range b {
		if _, ok := hash[v]; ok {
			set = append(set, v)
		}
	}

	return set
}

func GetCommonColumnIds(conv *internal.Conv, tableId string, colIds []string) []string {
	srcSchema := conv.SrcSchema[tableId]
	var commonColIds []string
	for i, colId := range colIds {
		_, found := srcSchema.ColDefs[colId]
		if found {
			commonColIds = append(commonColIds, colIds[i])
		}
	}
	return commonColIds
}

func PrepareColumns(conv *internal.Conv, tableId string, srcCols []string) ([]string, error) {
	spColIds := conv.SpSchema[tableId].ColIds
	srcColIds := []string{}
	for _, colName := range srcCols {
		colId, err := internal.GetColIdFromSrcName(conv.SrcSchema[tableId].ColDefs, colName)
		if err != nil {
			return []string{}, err
		}
		srcColIds = append(srcColIds, colId)
	}
	commonIds := IntersectionOfTwoStringSlices(spColIds, srcColIds)
	if len(commonIds) == 0 {
		return []string{}, fmt.Errorf("no common columns between source and spanner table")
	}
	return commonIds, nil
}

func PrepareValues[T interface{}](conv *internal.Conv, tableId string, colNameIdMap map[string]string, commonColIds, srcCols []string, values []T) ([]T, error) {
	if len(srcCols) != len(values) {
		return []T{}, fmt.Errorf("PrepareValues: srcCols and vals don't all have the same lengths: len(srcCols)=%d, len(values)=%d", len(srcCols), len(values))
	}
	var newValues []T
	mapColIdToVal := map[string]T{}
	for i, srcolName := range srcCols {
		mapColIdToVal[colNameIdMap[srcolName]] = values[i]
	}
	for _, id := range commonColIds {
		newValues = append(newValues, mapColIdToVal[id])
	}
	return newValues, nil
}

func ToPGDialectType(standardType ddl.Type, isPk bool) (ddl.Type, []internal.SchemaIssue) {
	if standardType.IsArray {
		return ddl.Type{Name: ddl.String, Len: ddl.MaxLength, IsArray: false},
			[]internal.SchemaIssue{internal.ArrayTypeNotSupported}
	}
	if isPk && standardType.Name == ddl.Numeric {
		return ddl.Type{Name: ddl.String, Len: ddl.MaxLength, IsArray: false},
			[]internal.SchemaIssue{internal.NumericPKNotSupported}
	}
	return standardType, nil
}

func IsPrimaryKey(colId string, table schema.Table) bool {
	for _, pk := range table.PrimaryKeys {
		if pk.ColId == colId {
			return true
		}
	}
	return false
}

// Data type sizes are referred from https://cloud.google.com/spanner/docs/reference/standard-sql/data-types#storage_size_for_data_types
var DATATYPE_TO_STORAGE_SIZE = map[string]int{
	ddl.Bool:      1,
	ddl.Date:      4,
	ddl.Float32:   4,
	ddl.Float64:   8,
	ddl.Int64:     8,
	ddl.JSON:      ddl.StringMaxLength,
	ddl.Numeric:   22,
	ddl.Timestamp: 12,
}

func getColumnSize(dataType string, length int64) int {
	if dataType == ddl.String {
		if length == ddl.MaxLength {
			return ddl.StringMaxLength
		}
		return int(length)
	} else if dataType == ddl.Bytes {
		if length == ddl.MaxLength {
			return ddl.BytesMaxLength
		}
		return int(length)
	}
	return DATATYPE_TO_STORAGE_SIZE[dataType]
}

func checkIfColumnIsPartOfPK(id string, primaryKey []schema.Key) bool {
	for _, key := range primaryKey {
		if key.ColId == id {
			return true
		}
	}
	return false
}

func checkIfColumnIsPartOfSpSchemaPK(id string, primaryKey []ddl.IndexKey) bool {
	for _, key := range primaryKey {
		if key.ColId == id {
			return true
		}
	}
	return false
}

func ComputeNonKeyColumnSize(conv *internal.Conv, tableId string) {
	totalNonKeyColumnSize := 0
	tableLevelIssues := conv.SchemaIssues[tableId].TableLevelIssues
	tableLevelIssues = removeSchemaIssue(tableLevelIssues, internal.RowLimitExceeded)
	for _, colDef := range conv.SpSchema[tableId].ColDefs {
		if !checkIfColumnIsPartOfSpSchemaPK(colDef.Id, conv.SpSchema[tableId].PrimaryKeys) {
			totalNonKeyColumnSize += getColumnSize(colDef.T.Name, colDef.T.Len)
		}
	}
	if totalNonKeyColumnSize > ddl.MaxNonKeyColumnLength {
		tableLevelIssues = append(tableLevelIssues, internal.RowLimitExceeded)
	}
	conv.SchemaIssues[tableId] = internal.TableIssues{
		TableLevelIssues:  tableLevelIssues,
		ColumnLevelIssues: conv.SchemaIssues[tableId].ColumnLevelIssues,
	}
}

// removeSchemaIssue removes issue from the given list.
func removeSchemaIssue(schemaissue []internal.SchemaIssue, issue internal.SchemaIssue) []internal.SchemaIssue {
	ind := findSchemaIssue(schemaissue, issue)
	if ind != -1 {
		return append(schemaissue[:ind], schemaissue[ind+1:]...)
	}
	return schemaissue
}

func findSchemaIssue(schemaissue []internal.SchemaIssue, issue internal.SchemaIssue) int {
	ind := -1
	for i := 0; i < len(schemaissue); i++ {
		if schemaissue[i] == issue {
			ind = i
		}
	}
	return ind
}
