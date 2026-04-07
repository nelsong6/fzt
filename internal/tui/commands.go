package tui

import "github.com/nelsong6/fzt/core"

// buildCoreItems creates the core fzt commands (version, update).
// parentIdx is the index of the parent item (-1 for root), baseIdx is the
// starting index in the items slice, and depth is the tree depth.
func buildCoreItems(parentIdx int, baseIdx int, depth int) []core.Item {
	// "version" folder (baseIdx+0) with on/off children (baseIdx+1, baseIdx+2)
	versionIdx := baseIdx
	items := []core.Item{
		{
			Fields:      []string{"version", "Show/hide version in title bar"},
			Depth:       depth,
			HasChildren: true,
			ParentIdx:   parentIdx,
			Children:    []int{baseIdx + 1, baseIdx + 2},
		},
		{
			Fields:    []string{"on", "Show version"},
			Depth:     depth + 1,
			ParentIdx: versionIdx,
		},
		{
			Fields:    []string{"off", "Hide version"},
			Depth:     depth + 1,
			ParentIdx: versionIdx,
		},
		// "update" leaf (baseIdx+3)
		{
			Fields:    []string{"update", "Update fzt to latest release"},
			Depth:     depth,
			ParentIdx: parentIdx,
		},
	}
	return items
}

// buildCommandItems creates tree items for the command palette.
// Frontend commands appear at root level. Core commands are nested inside a ":" subfolder.
// If no frontend commands are registered, core commands are promoted to root.
func buildCommandItems(s *core.State) []core.Item {
	hasFrontend := len(s.FrontendCommands) > 0

	if !hasFrontend {
		// No frontend commands — core commands at root
		return buildCoreItems(-1, 0, 0)
	}

	var items []core.Item

	// Frontend commands as root-level leaves
	for _, cmd := range s.FrontendCommands {
		items = append(items, core.Item{
			Fields:    []string{cmd.Name, cmd.Description},
			Depth:     0,
			ParentIdx: -1,
		})
	}

	// ":" subfolder for core commands
	coreFolderIdx := len(items)
	coreChildren := buildCoreItems(coreFolderIdx, coreFolderIdx+1, 1)

	// Build children indices for the ":" folder
	var childIndices []int
	for i := range coreChildren {
		if coreChildren[i].Depth == 1 {
			childIndices = append(childIndices, coreFolderIdx+1+i)
		}
	}

	items = append(items, core.Item{
		Fields:      []string{":", "fzt core commands"},
		Depth:       0,
		HasChildren: true,
		ParentIdx:   -1,
		Children:    childIndices,
	})

	items = append(items, coreChildren...)

	return items
}

// newCommandContext creates a treeContext for the command palette.
func newCommandContext(s *core.State) core.TreeContext {
	cmdItems := buildCommandItems(s)

	// Compute nameColWidth for command items
	nameColW := 0
	for _, item := range cmdItems {
		if len(item.Fields) > 0 {
			w := len([]rune(item.Fields[0]))
			if w > nameColW {
				nameColW = w
			}
		}
	}

	ctx := core.TreeContext{
		AllItems:      cmdItems,
		Items:         cmdItems,
		NameColWidth:  nameColW,
		ColGap:        2,
		Index:         -1,
		TreeExpanded:  make(map[int]bool),
		QueryExpanded: make(map[int]bool),
		TreeCursor:    -1,
		Scope:         []core.ScopeLevel{{ParentIdx: -1}},
		Kind:          core.ContextCommand,
		PromptIcon:    ':',
		OnLeafSelect: func(item core.Item) string {
			if len(item.Fields) == 0 {
				return ""
			}
			name := item.Fields[0]

			// Check core commands
			switch name {
			case "on":
				s.ShowVersion = true
				s.PopContext()
				return ""
			case "off":
				s.ShowVersion = false
				s.PopContext()
				return ""
			case "update":
				s.PopContext()
				return "update"
			}

			// Check frontend commands by name → return action
			for _, cmd := range s.FrontendCommands {
				if cmd.Name == name {
					s.PopContext()
					return cmd.Action
				}
			}

			s.PopContext()
			return ""
		},
	}

	return ctx
}
