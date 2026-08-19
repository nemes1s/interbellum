package integration

import (
	"github.com/google/uuid"

	"github.com/indurex/interbellum/internal/domain/playbook"
)

// newForkPlaybook builds the minimal branching graph used by the concurrency
// tests: one decision node with two labelled choices, each leading to its own
// terminal outcome.
func newForkPlaybook(
	root, yesNode, noNode, edgeYes, edgeNo uuid.UUID,
	yesResolution, noResolution string,
) playbook.NewPlaybook {
	return playbook.NewPlaybook{
		Name:      "Fork " + uuid.NewString(),
		AlertType: "test_type",
		Graph: playbook.Graph{
			RootNodeID: &root,
			Nodes: []playbook.Node{
				{ID: root, Kind: playbook.KindDecision, Title: "Which branch?"},
				{ID: yesNode, Kind: playbook.KindTerminal, Title: "Yes branch", TerminalResolution: &yesResolution},
				{ID: noNode, Kind: playbook.KindTerminal, Title: "No branch", TerminalResolution: &noResolution},
			},
			Edges: []playbook.Edge{
				{ID: edgeYes, FromNodeID: root, ToNodeID: yesNode, Label: "Yes", SortOrder: 1},
				{ID: edgeNo, FromNodeID: root, ToNodeID: noNode, Label: "No", SortOrder: 2},
			},
		},
	}
}
