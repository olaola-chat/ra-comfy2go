package graphapi

import (
	"encoding/json"
	"io"
	"log"
	"os"
	"sort"
	"strconv"
)

// allow us to order nodes by thier execution order (ordinality)
type ByGraphOrdinal []*GraphNode

func (a ByGraphOrdinal) Len() int           { return len(a) }
func (a ByGraphOrdinal) Swap(i, j int)      { a[i], a[j] = a[j], a[i] }
func (a ByGraphOrdinal) Less(i, j int) bool { return a[i].Order < a[j].Order }

type Graph struct {
	Nodes                 []*GraphNode       `json:"nodes"`
	Links                 []*Link            `json:"links"`
	Groups                []*Group           `json:"groups"`
	LastNodeID            int                `json:"last_node_id"`
	LastLinkID            int                `json:"last_link_id"`
	Version               float32            `json:"version"`
	NodesByID             map[int]*GraphNode `json:"-"`
	LinksByID             map[int]*Link      `json:"-"`
	NodesInExecutionOrder []*GraphNode       `json:"-"`
	HasErrors             bool               `json:"-"`
}

// GetGroupWithTitle returns the 'first' group with the given title
func (t *Graph) GetGroupWithTitle(title string) *Group {
	for _, g := range t.Groups {
		if g.Title == title {
			return g
		}
	}
	return nil
}

func (t *Graph) GetNodesInGroup(g *Group) []*GraphNode {
	retv := make([]*GraphNode, 0)
	for _, n := range t.Nodes {
		if g.IntersectsOrContains(n) {
			retv = append(retv, n)
		}
	}
	return retv
}

func (t *Graph) UnmarshalJSON(b []byte) error {
	// Create an alias type to avoid recursive call to UnmarshalJSON
	type Alias Graph

	alias := &Alias{}

	if err := json.Unmarshal(b, alias); err != nil {
		return err
	}

	// Copy the fields from the alias to the original struct
	t.Nodes = alias.Nodes
	t.Links = alias.Links
	t.Groups = alias.Groups
	t.LastNodeID = alias.LastNodeID
	t.LastLinkID = alias.LastLinkID
	t.Version = alias.Version
	t.NodesByID = make(map[int]*GraphNode)
	t.LinksByID = make(map[int]*Link)

	for _, node := range t.Nodes {
		// Populate the "by ID's"
		t.NodesByID[node.ID] = node
		// Give the node a pointer to it's parent graph
		t.NodesByID[node.ID].Graph = t
	}

	for _, link := range t.Links {
		t.LinksByID[link.ID] = link
	}

	// get the ordinality of nodes
	t.NodesInExecutionOrder = make([]*GraphNode, len(t.Nodes))
	copy(t.NodesInExecutionOrder, t.Nodes)
	sort.Sort(ByGraphOrdinal(t.NodesInExecutionOrder))

	return nil
}

func duplicateProperty(prop Property) Property {
	switch prop.TypeString() {
	case "STRING":
		np := *prop.(*StringProperty)
		return &np
	case "FLOAT":
		np := *prop.(*FloatProperty)
		return &np
	case "COMBO":
		np := *prop.(*ComboProperty)
		return &np
	case "INT":
		np := *prop.(*IntProperty)
		return &np
	case "UNKNOWN":
		np := *prop.(*UnknownProperty)
		return &np
	}
	log.Println("Cannot duplicate property of unknown type")
	return nil
}

func (t *Graph) CreateNodeProperties(node_objects *NodeObjects) {
	// we'll store primitives and process them after all other nodes have
	// had thier properties created
	primitives := make([]*GraphNode, 0)
	for _, n := range t.Nodes {
		// random numbers seem to have an additional widget added in widget.js addValueControlWidget @ln 15
		// when an INT widget is created with either the name "seed" or "noise_seed", the additional
		// widget is added directly after.
		// it is a COMBO called "control_after_randomize" with one of:
		// 	fixed
		//	increment
		//	decrement
		// 	randomize

		// create a new map to hold the properties by name
		n.Properties = make(map[string]Property)
		nobject := node_objects.GetNodeObjectByName(n.Type)

		if nobject != nil {
			// get the display name and description
			n.DisplayName = nobject.DisplayName
			n.Description = nobject.Description

			props := nobject.GetSettableProperties()
			for pindex, prop := range props {
				// convert to actual property type, deep copy
				// store a pointer to the property in the node's
				// correct Input
				switch prop.TypeString() {
				case "STRING":
					np := *prop.(*StringProperty)
					np.UpdateParent(&np)
					np.SetTargetWidget(n, pindex)
					n.Properties[prop.Name()] = &np
					n.affixPropertyToInputSlot(prop.Name(), &np)
				case "FLOAT":
					np := *prop.(*FloatProperty)
					np.UpdateParent(&np)
					np.SetTargetWidget(n, pindex)
					n.Properties[prop.Name()] = &np
					n.affixPropertyToInputSlot(prop.Name(), &np)
				case "COMBO":
					np := *prop.(*ComboProperty)
					np.UpdateParent(&np)
					np.SetTargetWidget(n, pindex)
					n.Properties[prop.Name()] = &np
					n.affixPropertyToInputSlot(prop.Name(), &np)
				case "INT":
					np := *prop.(*IntProperty)
					np.UpdateParent(&np)
					np.SetTargetWidget(n, pindex)
					n.Properties[prop.Name()] = &np
					n.affixPropertyToInputSlot(prop.Name(), &np)
				case "BOOLEAN":
					np := *prop.(*BoolProperty)
					np.UpdateParent(&np)
					np.SetTargetWidget(n, pindex)
					n.Properties[prop.Name()] = &np
					n.affixPropertyToInputSlot(prop.Name(), &np)
				case "UNKNOWN":
					log.Println("UNKNOWN property type in settable field")
					np := *prop.(*UnknownProperty)
					np.UpdateParent(&np)
					np.SetTargetWidget(n, pindex)
					n.Properties[prop.Name()] = &np
					n.affixPropertyToInputSlot(prop.Name(), &np)
				}
			}
			if len(n.WidgetValues) != len(props) {
				// If the count of WidgetValues is not the same as props there may be potential issues
				// which may arrise here if not handled properly.  An example is LoadImage and LoadImageMask where
				// there is a widget "choose file to upload" whose field points to the
				// property that the upload would be set to.  This widget is added in web/extensions/core/uploadImage.js
				if nobject.Name == "LoadImage" || nobject.Name == "LoadImageMask" {
					// create an imageuploader property and point to it's associated COMBO property
					targetProp := n.GetPropertyWithName("image")
					if targetProp != nil {
						np := newImageUploadProperty("choose file to upload", targetProp.(*ComboProperty), len(n.Properties)-1)
						n.Properties["choose file to upload"] = *np
					} else {
						log.Println("Cannot find \"image\" property")
					}
				} else {
					log.Printf("size missmatch for %s\n", n.Type)
				}
			}
		} else {
			if n.Type == "PrimitiveNode" {
				primitives = append(primitives, n)
			} else if n.Type == "Note" || n.Type == "Reroute" {
				// skip Notes, Reroute
				continue
			} else {
				log.Printf("Could not get node object for %s\n", n.Type)
			}
		}
	}

	// process primitives
	// Can a primitive?:
	// 		Connect to reroute: 					Nope (thank god)
	//		Connect combo to two different types: 	Nope
	for _, primitive_node := range primitives {
		for _, primitive_node_output := range primitive_node.Outputs {
			// For outputs, we need to contend with multiple links.
			// Go through each output, get the link, then the target node,
			// then the target property of that node.
			if primitive_node_output.Links != nil && len(*primitive_node_output.Links) != 0 {
				// we'll use the type and value of primitive_node_output.Links[0].  I'll assume that.
				// the link IDs are ordered and [0] would be the first on linked
				var first_property Property
				for _, l := range *primitive_node_output.Links {
					primitive_node_output_link := t.GetLinkById(l)
					if primitive_node_output_link != nil {
						// get the target node
						target_node := t.GetNodeById(primitive_node_output_link.TargetID)
						if target_node != nil {
							if first_property == nil {
								first_property = target_node.Inputs[primitive_node_output_link.TargetSlot].Property
								if first_property == nil {
									log.Printf("Could not get primitive target slot property %s for node %s\n", target_node.Inputs[primitive_node_output_link.TargetSlot].Name, target_node.Title)
									continue
								}
								// copy the property and assign it the node's "value" property
								np := duplicateProperty(first_property)
								primitive_node.Properties["value"] = np
							} else {
								// copy the property and add the node's "value" property as a secondary
								p := target_node.Inputs[primitive_node_output_link.TargetSlot].Property
								if p != nil {
									newp := duplicateProperty(p)
									primitive_node.Properties["value"].AttachSecondaryProperty(newp)
								}
							}
						}
					}
				}
			}
		}
	}
}

func (t *Graph) GetLinkById(id int) *Link {
	val, ok := t.LinksByID[id]
	if ok {
		return val
	}
	return nil
}

func (t *Graph) GetNodeById(id int) *GraphNode {
	val, ok := t.NodesByID[id]
	if ok {
		return val
	}
	return nil
}

// GetNodesWithTitle retrieves nodes from the graph based on a given title. If a node's title is not set,
// it falls back to matching against the node's display name.
//
// Parameters:
//   - title: The title (or display name if title is absent) to filter nodes by.
//
// Returns:
//   - A slice of pointers to GraphNodes that match the specified title or display name.
func (t *Graph) GetNodesWithTitle(title string) []*GraphNode {
	retv := make([]*GraphNode, 0)
	for _, n := range t.Nodes {
		if (n.Title == "" && n.DisplayName == title) || n.Title == title {
			retv = append(retv, n)
		}
	}
	return retv
}

// GetFirstNodeWithTitle retrieves the first node from the graph based on a given title. If a node's title is not set,
// it falls back to matching against the node's display name.
//
// Parameters:
//   - title: The title (or display name if title is absent) to filter nodes by.
//
// Returns:
//   - A pointer to a GraphNode
func (t *Graph) GetFirstNodeWithTitle(title string) *GraphNode {
	nodes := t.GetNodesWithTitle(title)
	if len(nodes) != 0 {
		return nodes[0]
	}
	return nil
}

// GetNodesWithType retrieves all nodes in the graph that match a specified type.
//
// Parameters:
//   - nodeType: The type of node to filter by.
//
// Returns:
//   - A slice of pointers to GraphNodes that match the specified type.
func (t *Graph) GetNodesWithType(nodeType string) []*GraphNode {
	retv := make([]*GraphNode, 0)
	for _, n := range t.Nodes {
		if n.Type == nodeType {
			retv = append(retv, n)
		}
	}
	return retv
}

func NewGraphFromJsonReader(r io.Reader, node_objects *NodeObjects) (*Graph, error) {
	fileContent, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}

	// deserialize workflow into a graph
	text := string(fileContent)
	graph := &Graph{}
	err = json.Unmarshal([]byte(text), &graph)
	if err != nil {
		return nil, err
	}
	graph.CreateNodeProperties(node_objects)
	return graph, nil
}

func NewGraphFromJsonFile(path string, node_objects *NodeObjects) (*Graph, error) {
	freader, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer freader.Close()

	return NewGraphFromJsonReader(freader, node_objects)
}

func (t *Graph) GraphToPrompt(clientID string) (Prompt, error) {
	p := Prompt{
		ClientID: clientID,
		Nodes:    make(map[int]PromptNode),
		// PID:      "floopy-thingy-ma-bob", // we can add additionl information that is ignored by ComfyUI
	}
	for _, node := range t.NodesInExecutionOrder {
		if node.IsVirtual() {
			// Don't serialize frontend only nodes but let them make changes
			node.ApplyToGraph()
			continue
		}

		if node.Mode == 2 {
			// Don't serialize muted nodes
			continue
		}

		// create the prompt node
		pn := PromptNode{
			ClassType: node.Type,
			Inputs:    make(map[string]interface{}),
		}

		// populate the node input values
		for k, p := range node.Properties {
			if p.Serializable() {
				pn.Inputs[k] = p.GetValue()
			}
		}

		// populate the node input links
		for i, slot := range node.Inputs {
			parent := node.GetNodeForInput(i)
			if parent != nil {
				link := t.GetLinkById(slot.Link)
				for parent != nil && parent.IsVirtual() {
					link = parent.GetInputLink(link.OriginSlot)
					if link != nil {
						parent = parent.GetNodeForInput(link.OriginSlot)
					} else {
						break
					}
				}

				if link != nil {
					linfo := make([]interface{}, 2)
					linfo[0] = strconv.Itoa(link.OriginID)
					linfo[1] = link.OriginSlot
					pn.Inputs[node.Inputs[i].Name] = linfo
				}
			}
		}
		p.Nodes[node.ID] = pn
	}
	// assign our current graph as the workflow
	p.ExtraData.PngInfo.Workflow = t
	return p, nil
}