//go:build js && wasm

package main

import (
	"encoding/json"
	"sort"
	"strings"
	"syscall/js"

	"github.com/nelsong6/fzh/internal/model"
	"github.com/nelsong6/fzh/internal/scorer"
	"github.com/nelsong6/fzh/internal/yamlsrc"
)

var currentItems []model.Item

type jsItem struct {
	Name         string  `json:"name"`
	Description  string  `json:"description,omitempty"`
	Depth        int     `json:"depth"`
	HasChildren  bool    `json:"hasChildren"`
	Path         string  `json:"path"`
	ParentIdx    int     `json:"parentIdx"`
	Children     []int   `json:"children,omitempty"`
	Score        jsScore `json:"score"`
	MatchIndices [][]int `json:"matchIndices,omitempty"`
	Index        int     `json:"index"`
}

type jsScore struct {
	Name     int `json:"name"`
	Desc     int `json:"desc"`
	Ancestor int `json:"ancestor"`
}

func main() {
	js.Global().Set("fzh", js.ValueOf(map[string]interface{}{
		"loadYAML":    js.FuncOf(loadYAML),
		"filter":      js.FuncOf(filter),
		"getChildren": js.FuncOf(getChildren),
	}))
	select {}
}

func loadYAML(this js.Value, args []js.Value) interface{} {
	if len(args) < 1 {
		return jsError("loadYAML requires a YAML string argument")
	}
	items, err := yamlsrc.LoadFromString(args[0].String())
	if err != nil {
		return jsError(err.Error())
	}
	currentItems = items
	return toJSArray(itemsToJS(items, nil))
}

func filter(this js.Value, args []js.Value) interface{} {
	if len(args) < 1 {
		return jsError("filter requires a query string")
	}
	query := args[0].String()

	depthPenalty := 5
	parentIdx := -1
	if len(args) > 1 && args[1].Type() == js.TypeObject {
		opts := args[1]
		if v := opts.Get("depthPenalty"); v.Type() == js.TypeNumber {
			depthPenalty = v.Int()
		}
		if v := opts.Get("parentIdx"); v.Type() == js.TypeNumber {
			parentIdx = v.Int()
		}
	}

	if query == "" {
		return toJSArray(scopeItems(parentIdx))
	}

	searchPool := currentItems
	if parentIdx >= 0 {
		searchPool = descendantsOf(currentItems, parentIdx)
	}

	type scored struct {
		item    model.Item
		index   int
		ts      scorer.TieredScore
		indices [][]int
	}

	var matched []scored
	for i, item := range searchPool {
		idx := i
		if parentIdx >= 0 {
			idx = findOriginalIndex(item)
		}
		ancestors := getAncestorNames(currentItems, item)
		ts, indices := scorer.ScoreItem(item.Fields, query, nil, ancestors)
		if indices != nil {
			ts.Name -= item.Depth * depthPenalty
			item.Score = ts
			item.MatchIndices = indices
			matched = append(matched, scored{item, idx, ts, indices})
		}
	}

	sort.SliceStable(matched, func(i, j int) bool {
		return matched[j].ts.Less(matched[i].ts)
	})

	result := make([]jsItem, len(matched))
	for i, m := range matched {
		result[i] = modelToJS(m.item, m.index)
		result[i].Score = jsScore{m.ts.Name, m.ts.Desc, m.ts.Ancestor}
		result[i].MatchIndices = m.indices
	}
	return toJSArray(result)
}

func getChildren(this js.Value, args []js.Value) interface{} {
	if len(args) < 1 {
		return jsError("getChildren requires a parent index")
	}
	parentIdx := args[0].Int()
	return toJSArray(scopeItems(parentIdx))
}

func scopeItems(parentIdx int) []jsItem {
	var result []jsItem
	if parentIdx < 0 {
		for i, item := range currentItems {
			if item.Depth == 0 {
				result = append(result, modelToJS(item, i))
			}
		}
	} else if parentIdx < len(currentItems) {
		for _, childIdx := range currentItems[parentIdx].Children {
			if childIdx < len(currentItems) {
				result = append(result, modelToJS(currentItems[childIdx], childIdx))
			}
		}
	}
	return result
}

func descendantsOf(items []model.Item, parentIdx int) []model.Item {
	if parentIdx < 0 {
		return items
	}
	var result []model.Item
	var collect func(idx int)
	collect = func(idx int) {
		for _, childIdx := range items[idx].Children {
			if childIdx < len(items) {
				result = append(result, items[childIdx])
				collect(childIdx)
			}
		}
	}
	collect(parentIdx)
	return result
}

func findOriginalIndex(item model.Item) int {
	for i, ci := range currentItems {
		if ci.Path == item.Path && ci.Fields[0] == item.Fields[0] {
			return i
		}
	}
	return -1
}

func getAncestorNames(items []model.Item, item model.Item) []string {
	var names []string
	idx := item.ParentIdx
	seen := make(map[int]bool)
	for idx >= 0 && idx < len(items) && !seen[idx] {
		seen[idx] = true
		if len(items[idx].Fields) > 0 {
			names = append(names, items[idx].Fields[0])
		}
		idx = items[idx].ParentIdx
	}
	return names
}

func itemsToJS(items []model.Item, indices [][]int) []jsItem {
	result := make([]jsItem, len(items))
	for i, item := range items {
		result[i] = modelToJS(item, i)
	}
	return result
}

func modelToJS(item model.Item, index int) jsItem {
	ji := jsItem{
		Name:        item.Fields[0],
		Depth:       item.Depth,
		HasChildren: item.HasChildren,
		Path:        item.Path,
		ParentIdx:   item.ParentIdx,
		Children:    item.Children,
		Index:       index,
		Score:       jsScore{item.Score.Name, item.Score.Desc, item.Score.Ancestor},
	}
	if len(item.Fields) > 1 {
		ji.Description = strings.Join(item.Fields[1:], " ")
	}
	if item.MatchIndices != nil {
		ji.MatchIndices = item.MatchIndices
	}
	return ji
}

func toJSArray(items []jsItem) interface{} {
	data, _ := json.Marshal(items)
	return js.Global().Get("JSON").Call("parse", string(data))
}

func jsError(msg string) interface{} {
	return js.Global().Get("Error").New(msg)
}
