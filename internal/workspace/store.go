package workspace

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"restman/internal/id"
	"restman/internal/model"
)

type node struct {
	ID           string
	Kind         string
	Name         string
	ParentID     string
	CollectionID string
	Path         string
	Request      *model.Request
	Folder       *model.Folder
	Collection   *model.Collection
}

type Store struct {
	Root    string
	Meta    model.WorkspaceMeta
	Globals model.Globals
	Envs    map[string]*model.Environment
	envPath map[string]string
	nodes   map[string]*node
	ordered []string
}

func Open(root string) (*Store, error) {
	store := &Store{
		Root:    root,
		Envs:    map[string]*model.Environment{},
		envPath: map[string]string{},
		nodes:   map[string]*node{},
	}
	if err := os.MkdirAll(filepath.Join(root, "collections"), 0o755); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Join(root, "environments"), 0o755); err != nil {
		return nil, err
	}
	if err := store.load(); err != nil {
		return nil, err
	}
	return store, nil
}

func (s *Store) load() error {
	metaPath := filepath.Join(s.Root, "restman.json")
	if _, err := os.Stat(metaPath); os.IsNotExist(err) {
		s.Meta = model.WorkspaceMeta{Name: "Restman"}
		if err := writeJSON(metaPath, s.Meta); err != nil {
			return err
		}
	} else if err := readJSON(metaPath, &s.Meta); err != nil {
		return err
	}
	globalsPath := filepath.Join(s.Root, "globals.json")
	if _, err := os.Stat(globalsPath); os.IsNotExist(err) {
		s.Globals = model.Globals{Variables: []model.KeyValue{}}
		if err := writeJSON(globalsPath, s.Globals); err != nil {
			return err
		}
	} else if err := readJSON(globalsPath, &s.Globals); err != nil {
		return err
	}
	if s.Globals.Variables == nil {
		s.Globals.Variables = []model.KeyValue{}
	}

	envDir := filepath.Join(s.Root, "environments")
	entries, err := os.ReadDir(envDir)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		path := filepath.Join(envDir, entry.Name())
		var env model.Environment
		if err := readJSON(path, &env); err != nil {
			return err
		}
		if env.Variables == nil {
			env.Variables = []model.KeyValue{}
		}
		s.Envs[env.ID] = &env
		s.envPath[env.ID] = path
	}

	colDir := filepath.Join(s.Root, "collections")
	cols, err := os.ReadDir(colDir)
	if err != nil {
		return err
	}
	for _, entry := range cols {
		if !entry.IsDir() {
			continue
		}
		if err := s.loadCollection(filepath.Join(colDir, entry.Name())); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) loadCollection(dir string) error {
	var col model.Collection
	path := filepath.Join(dir, "collection.json")
	if err := readJSON(path, &col); err != nil {
		return err
	}
	if col.Auth.Type == "" {
		col.Auth.Type = model.AuthNone
	}
	s.nodes[col.ID] = &node{ID: col.ID, Kind: "collection", Name: col.Name, CollectionID: col.ID, Path: path, Collection: &col}
	return s.walk(dir, col.ID, col.ID)
}

func (s *Store) walk(dir, parentID, collectionID string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		path := filepath.Join(dir, entry.Name())
		if entry.IsDir() {
			var folder model.Folder
			if err := readJSON(filepath.Join(path, "folder.json"), &folder); err != nil {
				return err
			}
			if folder.Auth.Type == "" {
				folder.Auth.Type = model.AuthInherit
			}
			s.nodes[folder.ID] = &node{
				ID: folder.ID, Kind: "folder", Name: folder.Name, ParentID: parentID,
				CollectionID: collectionID, Path: filepath.Join(path, "folder.json"), Folder: &folder,
			}
			if err := s.walk(path, folder.ID, collectionID); err != nil {
				return err
			}
			continue
		}
		if strings.HasSuffix(entry.Name(), ".request.json") {
			var req model.Request
			if err := readJSON(path, &req); err != nil {
				return err
			}
			req.Normalize()
			s.nodes[req.ID] = &node{
				ID: req.ID, Kind: "request", Name: req.Name, ParentID: parentID,
				CollectionID: collectionID, Path: path, Request: &req,
			}
		}
	}
	return nil
}

func (s *Store) Tree() []model.TreeNode {
	children := map[string][]*node{}
	var roots []*node
	for _, n := range s.nodes {
		if n.Kind == "collection" {
			roots = append(roots, n)
			continue
		}
		children[n.ParentID] = append(children[n.ParentID], n)
	}
	sort.Slice(roots, func(i, j int) bool { return roots[i].Name < roots[j].Name })
	out := make([]model.TreeNode, 0, len(roots))
	for _, root := range roots {
		out = append(out, toTree(root, children))
	}
	return out
}

func toTree(n *node, children map[string][]*node) model.TreeNode {
	node := model.TreeNode{ID: n.ID, Name: n.Name, Kind: n.Kind}
	if n.Request != nil {
		node.Method = n.Request.Method
		node.Protocol = n.Request.Protocol
		node.URL = n.Request.URL
	}
	if n.Collection != nil {
		node.Variables = n.Collection.Variables
	}
	if n.Folder != nil {
		node.Variables = n.Folder.Variables
	}
	kids := children[n.ID]
	sortSiblings(orderOf(n), kids)
	for _, kid := range kids {
		node.Children = append(node.Children, toTree(kid, children))
	}
	return node
}

func orderOf(n *node) []string {
	if n == nil {
		return nil
	}
	if n.Collection != nil {
		return n.Collection.Order
	}
	if n.Folder != nil {
		return n.Folder.Order
	}
	return nil
}

func sortSiblings(order []string, kids []*node) {
	rank := map[string]int{}
	for i, id := range order {
		rank[id] = i
	}
	sort.SliceStable(kids, func(i, j int) bool {
		if len(order) > 0 {
			ri, iok := rank[kids[i].ID]
			rj, jok := rank[kids[j].ID]
			if iok && jok && ri != rj {
				return ri < rj
			}
			if iok != jok {
				return iok
			}
		}
		if kids[i].Kind != kids[j].Kind {
			return kids[i].Kind != "request"
		}
		return kids[i].Name < kids[j].Name
	})
}

func (s *Store) childrenOf(parentID string) []*node {
	var kids []*node
	for _, n := range s.nodes {
		if n.ParentID == parentID {
			kids = append(kids, n)
		}
	}
	sortSiblings(orderOf(s.nodes[parentID]), kids)
	return kids
}

func (s *Store) writeOrder(parentID string, ids []string) error {
	parent := s.nodes[parentID]
	if parent == nil {
		return fmt.Errorf("parent not found")
	}
	if parent.Collection != nil {
		parent.Collection.Order = ids
		return writeJSON(parent.Path, parent.Collection)
	}
	if parent.Folder != nil {
		parent.Folder.Order = ids
		return writeJSON(parent.Path, parent.Folder)
	}
	return fmt.Errorf("parent not found")
}

func (s *Store) Move(id, parentID string, index int) error {
	n, ok := s.nodes[id]
	if !ok || (n.Kind != "request" && n.Kind != "folder") {
		return fmt.Errorf("item not found")
	}
	parent, ok := s.nodes[parentID]
	if !ok || (parent.Kind != "collection" && parent.Kind != "folder") {
		return fmt.Errorf("parent not found")
	}
	if n.Kind == "folder" && (parentID == id || s.under(parentID, id)) {
		return fmt.Errorf("cannot move a folder into itself")
	}
	oldParent := n.ParentID
	if oldParent != parentID {
		base := filepath.Dir(parent.Path)
		if n.Kind == "folder" {
			oldDir := filepath.Dir(n.Path)
			destDir := filepath.Join(base, filepath.Base(oldDir))
			if oldDir != destDir {
				if _, err := os.Stat(destDir); err == nil {
					destDir = filepath.Join(base, s.uniqueSlug(base, n.Name, true))
				}
				if err := os.Rename(oldDir, destDir); err != nil {
					return err
				}
				s.rewritePrefix(oldDir, destDir)
			}
			n.ParentID = parentID
			n.CollectionID = parent.CollectionID
			s.retarget(n.ID, parent.CollectionID)
			goto ordered
		}
		dest := filepath.Join(base, filepath.Base(n.Path))
		if dest != n.Path {
			if _, err := os.Stat(dest); err == nil {
				dest = filepath.Join(base, s.uniqueSlug(base, n.Name, false)+".request.json")
			}
			if err := os.Rename(n.Path, dest); err != nil {
				return err
			}
			n.Path = dest
		}
		n.ParentID = parentID
		n.CollectionID = parent.CollectionID
	}
ordered:
	ids := make([]string, 0)
	for _, kid := range s.childrenOf(parentID) {
		if kid.ID != id {
			ids = append(ids, kid.ID)
		}
	}
	if index < 0 {
		index = 0
	}
	if index > len(ids) {
		index = len(ids)
	}
	next := append(append(append([]string{}, ids[:index]...), id), ids[index:]...)
	if err := s.writeOrder(parentID, next); err != nil {
		return err
	}
	if oldParent != parentID && oldParent != "" {
		remaining := make([]string, 0)
		for _, kid := range s.childrenOf(oldParent) {
			remaining = append(remaining, kid.ID)
		}
		if err := s.writeOrder(oldParent, remaining); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) under(id, ancestor string) bool {
	for id != "" {
		n := s.nodes[id]
		if n == nil {
			return false
		}
		if n.ParentID == ancestor {
			return true
		}
		id = n.ParentID
	}
	return false
}

func (s *Store) rewritePrefix(oldDir, newDir string) {
	oldDir = filepath.Clean(oldDir)
	newDir = filepath.Clean(newDir)
	prefix := oldDir + string(os.PathSeparator)
	for _, n := range s.nodes {
		if n.Path == oldDir || strings.HasPrefix(n.Path, prefix) {
			n.Path = newDir + strings.TrimPrefix(filepath.Clean(n.Path), oldDir)
		}
	}
}

func (s *Store) retarget(id, collectionID string) {
	for _, n := range s.nodes {
		if n.ID == id || s.under(n.ID, id) {
			n.CollectionID = collectionID
		}
	}
}

func (s *Store) CreateCollection(name string) (*model.Collection, error) {
	col := &model.Collection{
		ID: id.New(), Name: name, Auth: model.Auth{Type: model.AuthNone},
		Variables: []model.KeyValue{}, Scripts: model.Scripts{},
	}
	dir := filepath.Join(s.Root, "collections", s.uniqueSlug(filepath.Join(s.Root, "collections"), name, true))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	path := filepath.Join(dir, "collection.json")
	if err := writeJSON(path, col); err != nil {
		return nil, err
	}
	s.nodes[col.ID] = &node{ID: col.ID, Kind: "collection", Name: col.Name, CollectionID: col.ID, Path: path, Collection: col}
	return col, nil
}

func (s *Store) CreateFolder(parentID, name string) (*model.Folder, error) {
	parent, ok := s.nodes[parentID]
	if !ok || (parent.Kind != "collection" && parent.Kind != "folder") {
		return nil, fmt.Errorf("parent not found")
	}
	folder := &model.Folder{ID: id.New(), Name: name, Auth: model.Auth{Type: model.AuthInherit}}
	base := filepath.Dir(parent.Path)
	dir := filepath.Join(base, s.uniqueSlug(base, name, true))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	path := filepath.Join(dir, "folder.json")
	if err := writeJSON(path, folder); err != nil {
		return nil, err
	}
	s.nodes[folder.ID] = &node{
		ID: folder.ID, Kind: "folder", Name: name, ParentID: parentID,
		CollectionID: parent.CollectionID, Path: path, Folder: folder,
	}
	return folder, nil
}

func (s *Store) CreateRequest(parentID, name, protocol string) (*model.Request, error) {
	parent, ok := s.nodes[parentID]
	if !ok || (parent.Kind != "collection" && parent.Kind != "folder") {
		return nil, fmt.Errorf("parent not found")
	}
	if protocol == "" {
		protocol = model.ProtocolHTTP
	}
	req := &model.Request{
		ID: id.New(), Name: name, Protocol: protocol, Method: "GET",
		Auth: model.Auth{Type: model.AuthInherit}, Body: model.Body{Mode: model.BodyNone},
		Params: []model.KeyValue{}, Headers: []model.KeyValue{},
	}
	req.Normalize()
	base := filepath.Dir(parent.Path)
	path := filepath.Join(base, s.uniqueSlug(base, name, false)+".request.json")
	if err := writeJSON(path, req); err != nil {
		return nil, err
	}
	s.nodes[req.ID] = &node{
		ID: req.ID, Kind: "request", Name: name, ParentID: parentID,
		CollectionID: parent.CollectionID, Path: path, Request: req,
	}
	return req, nil
}

func (s *Store) GetRequest(requestID string) (model.Request, error) {
	n, ok := s.nodes[requestID]
	if !ok || n.Request == nil {
		return model.Request{}, fmt.Errorf("request not found")
	}
	return *n.Request, nil
}

func (s *Store) SaveRequest(parentID string, req model.Request) error {
	req.Normalize()
	n, ok := s.nodes[req.ID]
	if !ok {
		if parentID == "" {
			return fmt.Errorf("choose a collection before saving")
		}
		parent, ok := s.nodes[parentID]
		if !ok {
			return fmt.Errorf("parent not found")
		}
		base := filepath.Dir(parent.Path)
		path := filepath.Join(base, s.uniqueSlug(base, req.Name, false)+".request.json")
		if err := writeJSON(path, &req); err != nil {
			return err
		}
		s.nodes[req.ID] = &node{
			ID: req.ID, Kind: "request", Name: req.Name, ParentID: parentID,
			CollectionID: parent.CollectionID, Path: path, Request: &req,
		}
		return nil
	}
	if err := writeJSON(n.Path, &req); err != nil {
		return err
	}
	n.Name = req.Name
	n.Request = &req
	return nil
}

func (s *Store) DuplicateNode(nodeID string) (string, error) {
	n, ok := s.nodes[nodeID]
	if !ok {
		return "", fmt.Errorf("item not found")
	}
	switch n.Kind {
	case "request":
		req, err := s.DuplicateRequest(nodeID)
		if err != nil {
			return "", err
		}
		return req.ID, nil
	case "collection":
		created, err := s.CreateCollection(n.Name + " copy")
		if err != nil {
			return "", err
		}
		created.Description = n.Collection.Description
		created.Auth = n.Collection.Auth
		created.Variables = append([]model.KeyValue{}, n.Collection.Variables...)
		created.Scripts = n.Collection.Scripts
		if err := s.SaveCollection(*created); err != nil {
			return "", err
		}
		for _, child := range s.childrenOf(nodeID) {
			if err := s.cloneInto(child.ID, created.ID); err != nil {
				return "", err
			}
		}
		return created.ID, nil
	case "folder":
		created, err := s.CreateFolder(n.ParentID, n.Name+" copy")
		if err != nil {
			return "", err
		}
		created.Description = n.Folder.Description
		created.Auth = n.Folder.Auth
		created.Variables = append([]model.KeyValue{}, n.Folder.Variables...)
		created.Scripts = n.Folder.Scripts
		if err := s.SaveFolder(*created); err != nil {
			return "", err
		}
		for _, child := range s.childrenOf(nodeID) {
			if err := s.cloneInto(child.ID, created.ID); err != nil {
				return "", err
			}
		}
		return created.ID, nil
	default:
		return "", fmt.Errorf("item not found")
	}
}

func (s *Store) cloneInto(nodeID, parentID string) error {
	n := s.nodes[nodeID]
	if n == nil {
		return fmt.Errorf("item not found")
	}
	if n.Kind == "request" {
		req := *n.Request
		req.ID = id.New()
		req.PostmanID = ""
		return s.SaveRequest(parentID, req)
	}
	created, err := s.CreateFolder(parentID, n.Name)
	if err != nil {
		return err
	}
	created.Description = n.Folder.Description
	created.Auth = n.Folder.Auth
	created.Variables = append([]model.KeyValue{}, n.Folder.Variables...)
	created.Scripts = n.Folder.Scripts
	if err := s.SaveFolder(*created); err != nil {
		return err
	}
	for _, child := range s.childrenOf(nodeID) {
		if err := s.cloneInto(child.ID, created.ID); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) DuplicateRequest(requestID string) (*model.Request, error) {
	n, ok := s.nodes[requestID]
	if !ok || n.Request == nil {
		return nil, fmt.Errorf("request not found")
	}
	copyReq := *n.Request
	copyReq.ID = id.New()
	copyReq.PostmanID = ""
	copyReq.Name = n.Request.Name + " copy"
	if err := s.SaveRequest(n.ParentID, copyReq); err != nil {
		return nil, err
	}
	saved, err := s.GetRequest(copyReq.ID)
	return &saved, err
}

func (s *Store) Delete(nodeID string) error {
	n, ok := s.nodes[nodeID]
	if !ok {
		return fmt.Errorf("item not found")
	}
	target := n.Path
	if n.Kind != "request" {
		target = filepath.Dir(n.Path)
	}
	if n.ParentID != "" {
		if parent := s.nodes[n.ParentID]; parent != nil && len(orderOf(parent)) > 0 {
			remaining := make([]string, 0)
			for _, item := range orderOf(parent) {
				if item != nodeID {
					remaining = append(remaining, item)
				}
			}
			if err := s.writeOrder(n.ParentID, remaining); err != nil {
				return err
			}
		}
	}
	if err := os.RemoveAll(target); err != nil {
		return err
	}
	s.drop(nodeID)
	return nil
}

func (s *Store) drop(nodeID string) {
	var kids []string
	for id, n := range s.nodes {
		if n.ParentID == nodeID {
			kids = append(kids, id)
		}
	}
	for _, kid := range kids {
		s.drop(kid)
	}
	delete(s.nodes, nodeID)
}

func (s *Store) Collection(id string) (model.Collection, bool) {
	n, ok := s.nodes[id]
	if !ok || n.Collection == nil {
		return model.Collection{}, false
	}
	return *n.Collection, true
}

func (s *Store) CollectionByExternalID(external string) (model.Collection, bool) {
	if external == "" {
		return model.Collection{}, false
	}
	for _, n := range s.nodes {
		if n.Kind != "collection" || n.Collection == nil {
			continue
		}
		if n.ID == external || n.Collection.PostmanID == external {
			return *n.Collection, true
		}
	}
	return model.Collection{}, false
}

func (s *Store) SaveCollection(col model.Collection) error {
	n, ok := s.nodes[col.ID]
	if !ok || n.Collection == nil {
		return fmt.Errorf("collection not found")
	}
	if err := writeJSON(n.Path, &col); err != nil {
		return err
	}
	n.Collection = &col
	n.Name = col.Name
	return nil
}

func (s *Store) Folder(id string) (model.Folder, bool) {
	n, ok := s.nodes[id]
	if !ok || n.Folder == nil {
		return model.Folder{}, false
	}
	return *n.Folder, true
}

func (s *Store) SaveFolder(folder model.Folder) error {
	n, ok := s.nodes[folder.ID]
	if !ok || n.Folder == nil {
		return fmt.Errorf("folder not found")
	}
	if err := writeJSON(n.Path, &folder); err != nil {
		return err
	}
	n.Folder = &folder
	n.Name = folder.Name
	return nil
}

func (s *Store) Ancestors(nodeID string) []string {
	var chain []string
	current := s.nodes[nodeID]
	for current != nil && current.ParentID != "" {
		chain = append([]string{current.ParentID}, chain...)
		current = s.nodes[current.ParentID]
	}
	return chain
}

func (s *Store) ParentID(nodeID string) string {
	n, ok := s.nodes[nodeID]
	if !ok {
		return ""
	}
	return n.ParentID
}

func (s *Store) CollectionID(nodeID string) string {
	n, ok := s.nodes[nodeID]
	if !ok {
		return ""
	}
	return n.CollectionID
}

func (s *Store) RequestsUnder(nodeID string) []model.Request {
	var out []model.Request
	var walk func(id string)
	walk = func(id string) {
		n := s.nodes[id]
		if n == nil {
			return
		}
		if n.Request != nil && n.Request.Protocol != model.ProtocolWebSocket {
			out = append(out, *n.Request)
		}
		for _, kid := range s.childrenOf(id) {
			walk(kid.ID)
		}
	}
	walk(nodeID)
	return out
}

func (s *Store) ActiveEnvironment() *model.Environment {
	if s.Meta.ActiveEnvironmentID == "" {
		return nil
	}
	return s.Envs[s.Meta.ActiveEnvironmentID]
}

func (s *Store) Environments() []model.Environment {
	out := make([]model.Environment, 0, len(s.Envs))
	for _, env := range s.Envs {
		out = append(out, *env)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func (s *Store) SaveEnvironment(env model.Environment) error {
	if env.ID == "" {
		env.ID = id.New()
	}
	if env.Variables == nil {
		env.Variables = []model.KeyValue{}
	}
	path, ok := s.envPath[env.ID]
	if !ok {
		path = filepath.Join(s.Root, "environments", s.uniqueSlug(filepath.Join(s.Root, "environments"), env.Name, false)+".json")
	}
	if err := writeJSON(path, &env); err != nil {
		return err
	}
	s.Envs[env.ID] = &env
	s.envPath[env.ID] = path
	return nil
}

func (s *Store) DeleteEnvironment(envID string) error {
	path, ok := s.envPath[envID]
	if !ok {
		return fmt.Errorf("environment not found")
	}
	if err := os.Remove(path); err != nil {
		return err
	}
	delete(s.Envs, envID)
	delete(s.envPath, envID)
	if s.Meta.ActiveEnvironmentID == envID {
		s.Meta.ActiveEnvironmentID = ""
		return s.SaveMeta()
	}
	return nil
}

func (s *Store) SetActiveEnvironment(envID string) error {
	if envID != "" {
		if _, ok := s.Envs[envID]; !ok {
			return fmt.Errorf("environment not found")
		}
	}
	s.Meta.ActiveEnvironmentID = envID
	return s.SaveMeta()
}

func (s *Store) SaveGlobals(vars []model.KeyValue) error {
	s.Globals.Variables = vars
	return writeJSON(filepath.Join(s.Root, "globals.json"), s.Globals)
}

func (s *Store) SaveMeta() error {
	return writeJSON(filepath.Join(s.Root, "restman.json"), s.Meta)
}

func (s *Store) uniqueSlug(dir, name string, asDir bool) string {
	base := id.Slug(name)
	candidate := base
	for i := 2; ; i++ {
		path := filepath.Join(dir, candidate)
		if !asDir {
			if strings.HasSuffix(dir, "environments") {
				path += ".json"
			} else {
				path += ".request.json"
			}
		}
		if _, err := os.Stat(path); os.IsNotExist(err) {
			return candidate
		}
		candidate = fmt.Sprintf("%s-%d", base, i)
	}
}

func readJSON(path string, dest any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, dest)
}

func writeJSON(path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
