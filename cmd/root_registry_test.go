package cmd

import "testing"

func TestCommandRegistryIsConsistent(t *testing.T) {
	seenGroups := make(map[string]struct{})
	seenCommands := make(map[string]string)

	for _, group := range commandGroups {
		if _, ok := seenGroups[group.group.ID]; ok {
			t.Fatalf("duplicate command group %q", group.group.ID)
		}
		seenGroups[group.group.ID] = struct{}{}

		for _, spec := range group.commands {
			if spec.build == nil {
				t.Fatalf("group %q has a nil command builder", group.group.ID)
			}
			if spec.capability.watchFlags && !spec.capability.workflowFlags {
				t.Fatalf("group %q has watch flags without workflow flags", group.group.ID)
			}

			name := spec.build(&options{}).Name()
			if previousGroup, ok := seenCommands[name]; ok {
				t.Fatalf("command %q is registered in both %q and %q", name, previousGroup, group.group.ID)
			}
			seenCommands[name] = group.group.ID
		}
	}
}
