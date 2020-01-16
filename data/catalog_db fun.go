package data

/*
 Copyright 2019 Crunchy Data Solutions, Inc.
 Licensed under the Apache License, Version 2.0 (the "License");
 you may not use this file except in compliance with the License.
 You may obtain a copy of the License at
      http://www.apache.org/licenses/LICENSE-2.0
 Unless required by applicable law or agreed to in writing, software
 distributed under the License is distributed on an "AS IS" BASIS,
 WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 See the License for the specific language governing permissions and
 limitations under the License.
*/

import (
	"context"
	"fmt"

	"github.com/jackc/pgtype"
	"github.com/jackc/pgx/v4"
	"github.com/jackc/pgx/v4/pgxpool"
	log "github.com/sirupsen/logrus"
)

func (cat *catalogDB) Functions() ([]*Function, error) {
	cat.refreshFunctions(true)
	return cat.functions, nil
}

func (cat *catalogDB) FunctionByName(name string) (*Function, error) {
	cat.refreshFunctions(false)
	fn, ok := cat.functionMap[name]
	if !ok {
		return nil, nil
	}
	return fn, nil
}

func (cat *catalogDB) refreshFunctions(force bool) {
	// TODO: refresh on timed basis?
	if force || !isFunctionsLoaded {
		cat.loadFunctions()
	}
	isFunctionsLoaded = true
}

func (cat *catalogDB) loadFunctions() {
	cat.functions, cat.functionMap = readFunctionDefs(cat.dbconn)
}

func readFunctionDefs(db *pgxpool.Pool) ([]*Function, map[string]*Function) {
	log.Debug(sqlFunctions)
	rows, err := db.Query(context.Background(), sqlFunctions)
	if err != nil {
		log.Fatal(err)
	}
	var functions []*Function
	functionMap := make(map[string]*Function)
	for rows.Next() {
		fn := readFunctionDef(rows)
		// TODO: for now only show geometry functions
		if hasOutGeometry(fn) {
			functions = append(functions, fn)
			functionMap[fn.ID] = fn
		}
	}
	// Check for errors from iterating over rows.
	if err := rows.Err(); err != nil {
		log.Fatal(err)
	}
	rows.Close()
	return functions, functionMap
}

func readFunctionDef(rows pgx.Rows) *Function {
	var (
		id, schema, name, description                              string
		inNamesTA, inTypesTA, inDefaultsTA, outNamesTA, outTypesTA pgtype.TextArray
	)

	err := rows.Scan(&id, &schema, &name, &description,
		&inNamesTA, &inTypesTA, &inDefaultsTA, &outNamesTA, &outTypesTA)
	if err != nil {
		log.Fatalf("Error reading function catalog: %v", err)
	}

	inNames := toArray(inNamesTA)
	inTypes := toArray(inTypesTA)
	inDefaults := toArray(inDefaultsTA)
	outNames := toArray(outNamesTA)
	outTypes := toArray(outTypesTA)

	datatypes := make(map[string]string)
	addTypes(datatypes, inNames, inTypes)
	addTypes(datatypes, inNames, inTypes)

	// synthesize a description if none provided
	if description == "" {
		description = fmt.Sprintf("The function %v", id)
	}

	geomCol := geometryColumn(outNames, outTypes)

	return &Function{
		ID:             id,
		Schema:         schema,
		Name:           name,
		Description:    description,
		InNames:        inNames,
		InTypes:        inTypes,
		InDefaults:     inDefaults,
		OutNames:       outNames,
		OutTypes:       outTypes,
		Types:          datatypes,
		GeometryColumn: geomCol,
	}
}
func addTypes(typeMap map[string]string, names []string, types []string) {
	for i, name := range names {
		typeMap[name] = types[i]
	}
}
func geometryColumn(names []string, types []string) string {
	// TODO: extract from outNames, outTypes
	return "geom"
}
func toArray(ta pgtype.TextArray) []string {
	arrLen := ta.Dimensions[0].Length
	arrStart := ta.Dimensions[0].LowerBound - 1

	arr := make([]string, arrLen)

	for i := arrStart; i < arrLen; i++ {
		val := ta.Elements[i].String
		arr[i] = val
	}
	return arr
}
func hasOutGeometry(fun *Function) bool {
	for _, typ := range fun.OutTypes {
		if typ == "geometry" {
			return true
		}
	}
	return false
}

func (cat *catalogDB) FunctionFeatures(name string, param QueryParam) ([]string, error) {
	fn, err := cat.FunctionByName(name)
	if err != nil || fn == nil {
		return nil, err
	}
	propCols := removeNames(fn.OutNames, fn.GeometryColumn, "")
	sql := sqlFunction(fn, propCols, param)
	log.Debug(sql)
	features, err := readFeatures(cat.dbconn, propCols, sql)
	return features, err
}
