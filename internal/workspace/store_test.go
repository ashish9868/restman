package workspace

import (
	"path/filepath"
	"testing"
)

func TestRoundTrip(t *testing.T) {
	root := filepath.Join(t.TempDir(), "ws")
	store, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	col, err := store.CreateCollection("Pet store")
	if err != nil {
		t.Fatal(err)
	}
	folder, err := store.CreateFolder(col.ID, "Pets")
	if err != nil {
		t.Fatal(err)
	}
	req, err := store.CreateRequest(folder.ID, "List pets", "http")
	if err != nil {
		t.Fatal(err)
	}
	req.URL = "{{base}}/pets"
	if err := store.SaveRequest(folder.ID, *req); err != nil {
		t.Fatal(err)
	}
	again, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := again.GetRequest(req.ID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.URL != "{{base}}/pets" {
		t.Fatalf("%+v", loaded)
	}
	tree := again.Tree()
	if len(tree) != 1 || len(tree[0].Children) != 1 || len(tree[0].Children[0].Children) != 1 {
		t.Fatalf("%+v", tree)
	}
}

func TestMoveAndReorder(t *testing.T) {
	root := filepath.Join(t.TempDir(), "ws")
	store, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	demo, err := store.CreateCollection("Demo")
	if err != nil {
		t.Fatal(err)
	}
	other, err := store.CreateCollection("Imported")
	if err != nil {
		t.Fatal(err)
	}
	folder, err := store.CreateFolder(other.ID, "Nested")
	if err != nil {
		t.Fatal(err)
	}
	first, err := store.CreateRequest(demo.ID, "Alpha", "http")
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.CreateRequest(demo.ID, "Beta", "http")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Move(second.ID, demo.ID, 0); err != nil {
		t.Fatal(err)
	}
	tree := store.Tree()
	var demoNode modelNode
	for _, node := range tree {
		if node.ID == demo.ID {
			demoNode = modelNode{node.Children[0].ID, node.Children[1].ID}
		}
	}
	if demoNode.a != second.ID || demoNode.b != first.ID {
		t.Fatalf("order %+v", tree)
	}
	if err := store.Move(first.ID, folder.ID, 0); err != nil {
		t.Fatal(err)
	}
	if store.ParentID(first.ID) != folder.ID || store.CollectionID(first.ID) != other.ID {
		t.Fatalf("parent %s collection %s", store.ParentID(first.ID), store.CollectionID(first.ID))
	}
	again, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	if again.ParentID(first.ID) != folder.ID {
		t.Fatal("move did not persist")
	}
	reloaded := again.Tree()
	for _, node := range reloaded {
		if node.ID == demo.ID && (len(node.Children) != 1 || node.Children[0].ID != second.ID) {
			t.Fatalf("demo %+v", node.Children)
		}
	}
}

type modelNode struct{ a, b string }
