package config

import (
	"errors"
	"fmt"
	"io/ioutil"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/elliotchance/orderedmap"
	"github.com/goccy/go-yaml"
	"github.com/k1LoW/glyph"
	"github.com/k1LoW/tbls/dict"
	"github.com/pasztorpisti/qs"
)

const Sep = ":"
const Esc = "\\"
const Q = "?"

var escRep = strings.NewReplacer(fmt.Sprintf("%s%s", Esc, Sep), "__NDIAG_REP__")
var unescRep = strings.NewReplacer("__NDIAG_REP__", fmt.Sprintf("%s%s", Esc, Sep))
var qRep = strings.NewReplacer(fmt.Sprintf("%s%s", Esc, Q), "__NDIAG_REP__")
var unqRep = strings.NewReplacer("__NDIAG_REP__", fmt.Sprintf("%s%s", Esc, Q))

const DefaultDocPath = "archdoc"

var DefaultConfigFilePaths = []string{"ndiag.yml"}
var DefaultDescPath = "ndiag.descriptions"

// DefaultFormat is the default diagram format
const DefaultFormat = "png"

type NNode interface {
	Id() string
	FullName() string
}

type Attr struct {
	Key   string
	Value string
}

type NEdge struct {
	Src      *Component
	Dst      *Component
	Desc     string
	Relation *Relation
	Attrs    []*Attr
}

type Layer struct {
	Name string
	Desc string
}

type Config struct {
	Name              string             `yaml:"name"`
	Desc              string             `yaml:"desc,omitempty"`
	DocPath           string             `yaml:"docPath"`
	DescPath          string             `yaml:"descPath,omitempty"`
	Graph             *Graph             `yaml:"graph,omitempty"`
	HideDiagrams      bool               `yaml:"hideDiagrams,omitempty"`
	HideLayers        bool               `yaml:"hideLayers,omitempty"`
	HideRealNodes     bool               `yaml:"hideRealNodes,omitempty"`
	HideTagGroups     bool               `yaml:"hideTagGroups,omitempty"`
	Diagrams          []*Diagram         `yaml:"diagrams"`
	Nodes             []*Node            `yaml:"nodes"`
	Relations         []*Relation        `yaml:"relations,omitempty"`
	Dict              *dict.Dict         `yaml:"dict,omitempty"`
	CustomIcons       []*glyph.Blueprint `yaml:"customIcons,omitempty"`
	rawRelations      []*rawRelation
	realNodes         []*RealNode
	layers            []*Layer
	clusters          Clusters
	globalComponents  []*Component
	clusterComponents []*Component
	nodeComponents    []*Component
	nEdges            []*NEdge
	tags              []*Tag
	iconMap           *glyph.Map
	tempIconDir       string
}

type Graph struct {
	Format        string        `yaml:"format,omitempty"`
	MapSliceAttrs yaml.MapSlice `yaml:"attrs,omitempty"`
}

func (g *Graph) Attrs() []*Attr {
	attrs := []*Attr{}
	for _, kv := range g.MapSliceAttrs {
		attrs = append(attrs, &Attr{
			Key:   kv.Key.(string),
			Value: kv.Value.(string),
		})
	}
	return attrs
}

func New() *Config {
	return &Config{
		Graph: &Graph{},
		Dict:  &dict.Dict{},
	}
}

func (cfg *Config) Format() string {
	if cfg.Graph.Format != "" {
		return cfg.Graph.Format
	}
	return DefaultFormat
}

func (cfg *Config) IconMap() *glyph.Map {
	return cfg.iconMap
}

func (cfg *Config) TempIconDir() string {
	return cfg.tempIconDir
}

func (cfg *Config) PrimaryDiagram() *Diagram {
	return cfg.Diagrams[0]
}

func (cfg *Config) Layers() []*Layer {
	return cfg.layers
}

func (cfg *Config) Clusters() Clusters {
	return cfg.clusters
}

func (cfg *Config) GlobalComponents() []*Component {
	return cfg.globalComponents
}

func (cfg *Config) ClusterComponents() []*Component {
	return cfg.clusterComponents
}

func (cfg *Config) NodeComponents() []*Component {
	return cfg.nodeComponents
}

func (cfg *Config) NEdges() []*NEdge {
	return cfg.nEdges
}

func (cfg *Config) Tags() []*Tag {
	return cfg.tags
}

func (cfg *Config) BuildNestedClusters(layers []string) (Clusters, []*Node, []*NEdge, error) {
	nEdges := []*NEdge{}
	if len(layers) == 0 {
		return Clusters{}, cfg.Nodes, cfg.nEdges, nil
	}
	clusters, globalNodes, err := buildNestedClusters(cfg.Clusters(), layers, cfg.Nodes)
	if err != nil {
		return clusters, globalNodes, nil, err
	}

	for _, e := range cfg.nEdges {
		hBelongTo := false
		tBelongTo := false
		for _, l := range layers {
			if e.Src.Cluster == nil || strings.EqualFold(e.Src.Cluster.Layer, l) {
				hBelongTo = true
			}
			if e.Dst.Cluster == nil || strings.EqualFold(e.Dst.Cluster.Layer, l) {
				tBelongTo = true
			}
		}
		if hBelongTo && tBelongTo {
			nEdges = append(nEdges, e)
		}
	}

	return clusters, globalNodes, nEdges, nil
}

func (cfg *Config) PruneClustersByTags(clusters Clusters, nodes []*Node, components []*Component, nEdges []*NEdge, tags []string) (Clusters, []*Node, []*Component, []*NEdge, error) {
	if len(tags) == 0 {
		return clusters, nodes, components, nEdges, nil
	}

	remainNEdges := []*NEdge{}
	nIds := orderedmap.NewOrderedMap()
	cIds := orderedmap.NewOrderedMap()
	comIds := orderedmap.NewOrderedMap()

	for _, name := range tags {
		t, err := cfg.FindTag(name)
		if err != nil {
			return clusters, nodes, components, nEdges, err
		}
		edges := SplitRelations(t.Relations)

		for _, e := range edges {
			switch {
			case e.Src.Node != nil:
				nIds.Set(e.Src.Node.Id(), e.Src.Node)
				for _, c := range e.Src.Node.Clusters {
					cIds.Set(c.Id(), c)
				}
			case e.Src.Cluster != nil:
				cIds.Set(e.Src.Cluster.Id(), e.Src.Cluster)
			}
			comIds.Set(e.Src.Id(), e.Src)

			switch {
			case e.Dst.Node != nil:
				nIds.Set(e.Dst.Node.Id(), e.Dst.Node)
				for _, c := range e.Dst.Node.Clusters {
					cIds.Set(c.Id(), c)
				}
			case e.Dst.Cluster != nil:
				cIds.Set(e.Dst.Cluster.Id(), e.Dst.Cluster)
			}
			comIds.Set(e.Dst.Id(), e.Dst)
		}

		remainNEdges = append(remainNEdges, edges...)
	}

	// prune cluster nodes
	pruneClusters(clusters, nIds, comIds)

	// global nodes
	filteredNodes := []*Node{}
	for _, n := range nodes {
		_, ok := nIds.Get(n.Id())
		if ok {
			filteredComponents := []*Component{}
			for _, c := range n.Components {
				_, ok := comIds.Get(c.Id())
				if ok {
					filteredComponents = append(filteredComponents, c)
				}
			}
			n.Components = filteredComponents
			filteredNodes = append(filteredNodes, n)
		}
	}
	nodes = filteredNodes

	// global components
	filteredComponents := []*Component{}
	for _, c := range components {
		_, ok := comIds.Get(c.Id())
		if ok {
			filteredComponents = append(filteredComponents, c)
		}
	}
	components = filteredComponents

	return clusters, nodes, components, remainNEdges, nil
}

func (cfg *Config) PruneNodesByTags(nodes []*Node, tags []string) ([]*Node, error) {
	if len(tags) == 0 {
		return nodes, nil
	}
	nIds := orderedmap.NewOrderedMap()
	comIds := orderedmap.NewOrderedMap()

	for _, name := range tags {
		t, err := cfg.FindTag(name)
		if err != nil {
			return nodes, nil
		}
		edges := SplitRelations(t.Relations)

		for _, e := range edges {
			switch {
			case e.Src.Node != nil:
				nIds.Set(e.Src.Node.Id(), e.Src.Node)
			}
			comIds.Set(e.Src.Id(), e.Src)

			switch {
			case e.Dst.Node != nil:
				nIds.Set(e.Dst.Node.Id(), e.Dst.Node)
			}
			comIds.Set(e.Dst.Id(), e.Dst)
		}
	}

	filteredNodes := []*Node{}
	for _, n := range nodes {
		_, ok := nIds.Get(n.Id())
		if ok {
			filteredComponents := []*Component{}
			for _, c := range n.Components {
				_, ok := comIds.Get(c.Id())
				if ok {
					filteredComponents = append(filteredComponents, c)
				}
			}
			n.Components = filteredComponents
			filteredNodes = append(filteredNodes, n)
		}
	}
	nodes = filteredNodes

	return nodes, nil
}

func (cfg *Config) LoadConfig(in []byte) error {
	if err := yaml.Unmarshal(in, cfg); err != nil {
		return err
	}
	return nil
}

func (cfg *Config) LoadConfigFile(path string) error {
	buf, err := loadFile(path)
	if err != nil {
		return err
	}
	return cfg.LoadConfig(buf)
}

func (cfg *Config) LoadRealNodes(in []byte) error {
	if len(cfg.Nodes) == 0 {
		return errors.New("nodes not found")
	}
	if err := cfg.loadRealNodes(in); err != nil {
		return err
	}
	return nil
}

func (cfg *Config) Build() error {
	if cfg.HideDiagrams && len(cfg.Diagrams) > 1 {
		return errors.New("can't make hideDiagrams true if you have more than one diagrams defined")
	}
	for _, n := range cfg.Nodes {
		if len(n.RealNodes) == 0 {
			return fmt.Errorf("'%s' does not have any real nodes", n.FullName())
		}
	}
	if err := cfg.buildIconMap(); err != nil {
		return err
	}
	if err := cfg.buildClusters(); err != nil {
		return err
	}
	if err := cfg.buildNodes(); err != nil {
		return err
	}
	if err := cfg.buildComponents(); err != nil {
		return err
	}
	if err := cfg.buildRelations(); err != nil {
		return err
	}
	if err := cfg.checkUnique(); err != nil {
		return err
	}
	if len(cfg.Diagrams) == 0 {
		cfg.Diagrams = append(cfg.Diagrams, &Diagram{
			Name:   "Nodes",
			Layers: []string{},
		})
	}
	// set default
	if cfg.DocPath == "" {
		cfg.DocPath = DefaultDocPath
	}
	if cfg.DescPath == "" {
		cfg.DescPath = DefaultDescPath
	}
	if err := cfg.buildDescriptions(); err != nil {
		return err
	}

	return nil
}

func (cfg *Config) loadRealNodes(in []byte) error {
	rNodes := []string{}
	if err := yaml.Unmarshal(in, &rNodes); err == nil {
		for _, rn := range rNodes {
			belongTo := false
			newRn := &RealNode{
				Node: Node{
					Name: rn,
				},
			}
		N:
			for _, n := range cfg.Nodes {
				if n.nameRe.MatchString(rn) {
					belongTo = true
					newRn.BelongTo = n
					n.RealNodes = append(n.RealNodes, newRn)
					break N
				}
			}
			if !belongTo {
				return fmt.Errorf("there is a real node '%s' that does not belong to a node", newRn.Name)
			}
			cfg.realNodes = append(cfg.realNodes, newRn)
		}
	} else {
		// config format
		rConfig := New()
		if err := yaml.Unmarshal(in, rConfig); err != nil {
			return err
		}
		for _, rn := range rConfig.Nodes {
			belongTo := false
			newRn := &RealNode{
				Node: *rn,
			}
		NN:
			for _, n := range cfg.Nodes {
				if n.nameRe.MatchString(rn.Name) {
					belongTo = true
					newRn.BelongTo = n
					n.RealNodes = append(n.RealNodes, newRn)
					n.rawClusters = merge(n.rawClusters, rn.rawClusters)
					n.rawComponents = merge(n.rawComponents, rn.rawComponents)
					break NN
				}
			}
			if !belongTo {
				return fmt.Errorf("there is a real node '%s' that does not belong to a node", newRn.Name)
			}
			cfg.realNodes = append(cfg.realNodes, newRn)
		}
		// replace component id ( real node name -> node name )
		for _, rel := range rConfig.rawRelations {
			replaced := []string{}
			for _, c := range rel.Components {
				splitted := sepSplit(c)
				if len(splitted) == 2 {
				RL:
					for _, n := range cfg.Nodes {
						if n.nameRe.MatchString(splitted[0]) {
							splitted[0] = n.Name
							break RL
						}
					}
				}
				replaced = append(replaced, sepJoin(splitted))
			}
			rel.Components = replaced
		}
		cfg.rawRelations = append(cfg.rawRelations, uniqueRawRelations(rConfig.rawRelations)...)
	}
	return nil
}

func (cfg *Config) LoadRealNodesFile(path string) error {
	buf, err := loadFile(path)
	if err != nil {
		return err
	}
	return cfg.LoadRealNodes(buf)
}

func (cfg *Config) FindNode(name string) (*Node, error) {
	for _, n := range cfg.Nodes {
		if strings.EqualFold(n.FullName(), name) {
			return n, nil
		}
	}
	return nil, fmt.Errorf("node not found: %s", name)
}

func (cfg *Config) FindComponent(s string) (*Component, error) {
	splited := querySplit(s)
	name := splited[0]
	var components []*Component
	switch sepCount(name) {
	case 2: // cluster components
		components = cfg.clusterComponents
	case 1: // node components
		components = cfg.nodeComponents
	case 0: // global components
		components = cfg.globalComponents
	}
	for _, c := range components {
		if strings.EqualFold(c.FullName(), name) {
			return c, nil
		}
	}
	return nil, fmt.Errorf("component not found: %s", name)
}

func (cfg *Config) FindTag(name string) (*Tag, error) {
	for _, t := range cfg.Tags() {
		if t.Name == name {
			return t, nil
		}
	}
	return nil, fmt.Errorf("tag not found: %s", name)
}

func (cfg *Config) buildNodes() error {
	for _, n := range cfg.Nodes {
		if n.Metadata.Icon != "" {
			n.Metadata.IconPath = filepath.Join(cfg.TempIconDir(), fmt.Sprintf("%s.png", n.Metadata.Icon))
		}
	}
	return nil
}

func (cfg *Config) buildComponents() error {
	gc := orderedmap.NewOrderedMap()
	nc := orderedmap.NewOrderedMap()
	cc := orderedmap.NewOrderedMap()
	for _, rel := range cfg.rawRelations {
		for _, r := range rel.Components {
			switch sepCount(r) {
			case 2: // cluster components
				cc.Set(r, struct{}{})
			case 1: // node components
				nc.Set(r, struct{}{})
			case 0: // global components
				gc.Set(r, struct{}{})
			}
		}
	}

	// global components
	for _, c := range gc.Keys() {
		com, err := cfg.parseComponent(c.(string))
		if err != nil {
			return err
		}
		// create global component from relations
		cfg.globalComponents = append(cfg.globalComponents, com)
	}

	// node components
	for _, n := range cfg.Nodes {
		for _, c := range n.rawComponents {
			com, err := cfg.parseComponent(c)
			if err != nil {
				return err
			}
			com.Node = n
			n.Components = append(n.Components, com)
		}
		cfg.nodeComponents = append(cfg.nodeComponents, n.Components...)
	}

	for _, c := range nc.Keys() {
		belongTo := false
		splitted := sepSplit(c.(string))
		nodeName := splitted[0]
		comName := splitted[1]
		n, err := cfg.FindNode(nodeName)
		if err != nil {
			return fmt.Errorf("node '%s' not found: %s", nodeName, c)
		}
		newCom, err := cfg.parseComponent(comName)
		if err != nil {
			return err
		}
		newCom.Node = n
		for _, com := range n.Components {
			if strings.EqualFold(com.FullName(), newCom.FullName()) {
				belongTo = true
				break
			}
		}
		if !belongTo {
			// create node component from relations
			n.Components = append(n.Components, newCom)
			cfg.nodeComponents = append(cfg.nodeComponents, newCom)
		}
	}

	// cluster components
	for _, c := range cc.Keys() {
		splitted := sepSplit(c.(string))
		clName := fmt.Sprintf("%s:%s", splitted[0], splitted[1])
		comName := splitted[2]
		belongTo := false
		for _, cl := range cfg.Clusters() {
			if strings.EqualFold(cl.FullName(), clName) {
				// create cluster component from relations
				com, err := cfg.parseComponent(comName)
				if err != nil {
					return err
				}
				com.Cluster = cl
				cl.Components = append(cl.Components, com)
				cfg.clusterComponents = append(cfg.clusterComponents, com)
				belongTo = true
				break
			}
		}
		if !belongTo {
			return fmt.Errorf("cluster '%s' not found: %s", clName, c)
		}
	}
	return nil
}

func (cfg *Config) buildClusters() error {
	for _, n := range cfg.Nodes {
		for _, c := range n.rawClusters {
			cluster, err := cfg.parseClusterLabel(c)
			if err != nil {
				return err
			}
			cluster.Nodes = append(cluster.Nodes, n)
			n.Clusters = append(n.Clusters, cluster)
		}
	}
	return nil
}

func (cfg *Config) parseClusterLabel(label string) (*Cluster, error) {
	if !strings.Contains(label, Sep) {
		return nil, fmt.Errorf("invalid cluster id: %s", label)
	}
	splitted := sepSplit(label)
	if len(splitted) != 2 {
		return nil, fmt.Errorf("invalid cluster id: %s", label)
	}
	layer := splitted[0]
	name := splitted[1]
	current := cfg.clusters.Find(layer, name)
	if current != nil {
		return current, nil
	}
	newC := &Cluster{
		Layer: layer,
		Name:  name,
	}
	cfg.clusters = append(cfg.clusters, newC)
	if !layerContains(cfg.layers, layer) {
		cfg.layers = append(cfg.layers, &Layer{Name: layer})
	}
	return newC, nil
}

func (cfg *Config) buildRelations() error {
	relTags := orderedmap.NewOrderedMap()
	for _, rel := range cfg.rawRelations {
		nrel := &Relation{
			relationId: rel.Id(),
			Type:       rel.Type,
			Tags:       rel.Tags,
			Attrs:      rel.Attrs,
		}
		for _, r := range rel.Components {
			c, err := cfg.FindComponent(r)
			if err != nil {
				return err
			}
			nrel.Components = append(nrel.Components, c)
		}
		cfg.Relations = append(cfg.Relations, nrel)

		// tags
		for _, t := range rel.Tags {
			var nt *Tag
			nti, ok := relTags.Get(t)
			if ok {
				nt = nti.(*Tag)
			} else {
				nt = &Tag{
					Name: t,
				}
				relTags.Set(t, nt)
			}
			nt.Relations = append(nt.Relations, nrel)
		}
	}
	cfg.nEdges = SplitRelations(cfg.Relations)

	for _, k := range relTags.Keys() {
		nt, _ := relTags.Get(k)
		cfg.tags = append(cfg.tags, nt.(*Tag))
	}

	return nil
}

func (cfg *Config) buildDescriptions() error {
	if cfg.DescPath == "" {
		return nil
	}
	err := os.MkdirAll(cfg.DescPath, 0755) // #nosec
	if err != nil {
		return err
	}

	// top
	if cfg.Desc == "" {
		desc, err := cfg.readDescFile(MdPath("_index", []string{}))
		if err != nil {
			return err
		}
		cfg.Desc = desc
	}

	// diagrams
	for _, d := range cfg.Diagrams {
		if d.Desc != "" {
			continue
		}
		desc, err := cfg.readDescFile(MdPath("_diagram", []string{d.Id()}))
		if err != nil {
			return err
		}
		d.Desc = desc
	}

	// clusters
	for _, c := range cfg.Clusters() {
		if c.Desc != "" {
			continue
		}
		desc, err := cfg.readDescFile(MdPath("_cluster", []string{c.Id()}))
		if err != nil {
			return err
		}
		c.Desc = desc
	}

	// layers
	for _, l := range cfg.Layers() {
		if l.Desc != "" {
			continue
		}
		desc, err := cfg.readDescFile(MdPath("_layer", []string{l.Name}))
		if err != nil {
			return err
		}
		l.Desc = desc
	}

	// nodes
	for _, n := range cfg.Nodes {
		if n.Desc != "" {
			continue
		}
		desc, err := cfg.readDescFile(MdPath("_node", []string{n.Id()}))
		if err != nil {
			return err
		}
		n.Desc = desc
	}

	// components
	for _, c := range cfg.GlobalComponents() {
		if c.Desc != "" {
			continue
		}
		desc, err := cfg.readDescFile(MdPath("_component", []string{c.Id()}))
		if err != nil {
			return err
		}
		c.Desc = desc
	}
	for _, c := range cfg.ClusterComponents() {
		if c.Desc != "" {
			continue
		}
		desc, err := cfg.readDescFile(MdPath("_component", []string{c.Id()}))
		if err != nil {
			return err
		}
		c.Desc = desc
	}
	for _, c := range cfg.NodeComponents() {
		if c.Desc != "" {
			continue
		}
		desc, err := cfg.readDescFile(MdPath("_component", []string{c.Id()}))
		if err != nil {
			return err
		}
		c.Desc = desc
	}

	// tags
	for _, t := range cfg.tags {
		if t.Desc != "" {
			continue
		}
		desc, err := cfg.readDescFile(MdPath("_tag", []string{t.Id()}))
		if err != nil {
			return err
		}
		t.Desc = desc
	}

	return nil
}

func (cfg *Config) readDescFile(f string) (string, error) {
	descPath := filepath.Join(cfg.DescPath, f)
	file, err := os.OpenFile(descPath, os.O_RDONLY|os.O_CREATE, 0644) // #nosec
	if err != nil {
		return "", err
	}
	b, err := ioutil.ReadAll(file)
	if err != nil {
		return "", err
	}
	if err := file.Close(); err != nil {
		return "", err
	}
	return string(b), err
}

func buildNestedClusters(clusters Clusters, layers []string, nodes []*Node) (Clusters, []*Node, error) {
	if len(layers) == 0 {
		return clusters, nodes, nil
	}
	leaf := layers[len(layers)-1]
	layers = layers[:len(layers)-1]

	remain := []*Node{}
	belongTo := []*Node{}
	for _, n := range nodes {
		c := n.Clusters.FindByLayer(leaf)
		if len(c) == 0 {
			remain = append(remain, n)
			continue
		}
		if len(c) > 1 {
			return nil, nil, fmt.Errorf("duplicate layer %s", leaf)
		}
		belongTo = append(belongTo, n)
		if len(layers) == 0 {
			continue
		}

		// build cluster tree using Node.Clusters
		parent := ""
		var pc Clusters
		for i := 1; i <= len(layers); i++ {
			parent = layers[len(layers)-i]
			pc = n.Clusters.FindByLayer(parent)
			if len(pc) > 1 {
				return nil, nil, fmt.Errorf("duplicate layer %s", parent)
			}
			if len(pc) == 0 {
				continue
			}
			c[0].Parent = pc[0]
			pc[0].Children = append(pc[0].Children, c[0])
			c = pc
		}
	}

	// build a direct member node of a cluster
	for _, c := range clusters {
		if c.Layer == leaf {
			continue
		}
		nodes := []*Node{}
	N:
		for _, n := range c.Nodes {
			for _, b := range belongTo {
				if n.FullName() == b.FullName() {
					continue N
				}
			}
			nodes = append(nodes, n)
		}
		c.Nodes = nodes
	}

	// build root clusters
	if len(layers) == 0 {
		root := Clusters{}
	NN:
		for _, c := range clusters {
			if c.Parent == nil && (c.Layer == leaf || len(c.Nodes) > 0) {
				for _, n := range c.Nodes {
					for _, rn := range remain {
						if n == rn {
							continue NN
						}
					}
				}
				root = append(root, c)
			}
		}
		clusters = root
	}

	return buildNestedClusters(clusters, layers, remain)
}

func (cfg *Config) buildIconMap() error {
	icm := glyph.NewMapWithIncluded(glyph.Width(80.0), glyph.Height(80.0))
	for _, i := range cfg.CustomIcons {
		g, k, err := i.ToGlyphAndKey()
		if err != nil {
			return err
		}
		icm.Set(k, g)
	}
	cfg.iconMap = icm
	rand.Seed(time.Now().UnixNano())
	r := rand.Intn(100000)
	cfg.tempIconDir = filepath.Join(os.TempDir(), fmt.Sprintf("ndiag.%06d", r))
	return nil
}

func (cfg *Config) checkUnique() error {

	ids := map[string]string{}

	// nodes
	for _, n := range cfg.Nodes {
		if t, exist := ids[n.Id()]; exist {
			return fmt.Errorf("duplicate id: %s[%s] <-> %s[%s]", t, n.Id(), "node", n.Id())
		}
		ids[n.Id()] = "node"
	}

	// components
	for _, c := range cfg.GlobalComponents() {
		if t, exist := ids[c.Id()]; exist {
			return fmt.Errorf("duplicate id: %s[%s] <-> %s[%s]", t, c.Id(), "component", c.Id())
		}
		ids[c.Id()] = "component"
	}
	for _, c := range cfg.ClusterComponents() {
		if t, exist := ids[c.Id()]; exist {
			return fmt.Errorf("duplicate id: %s[%s] <-> %s[%s]", t, c.Id(), "component", c.Id())
		}
		ids[c.Id()] = "component"
	}
	for _, c := range cfg.NodeComponents() {
		if t, exist := ids[c.Id()]; exist {
			return fmt.Errorf("duplicate id: %s[%s] <-> %s[%s]", t, c.Id(), "component", c.Id())
		}
		ids[c.Id()] = "component"
	}

	// clusters
	for _, c := range cfg.Clusters() {
		if t, exist := ids[c.Id()]; exist {
			return fmt.Errorf("duplicate id: %s[%s] <-> %s[%s]", t, c.Id(), "cluster", c.Id())
		}
		ids[c.Id()] = "cluster"
	}

	// read nodes
	m := map[string]struct{}{}
	for _, rn := range cfg.realNodes {
		if _, exist := m[rn.Name]; exist {
			return fmt.Errorf("duplicate real node name: %s", rn.Name)
		}
		m[rn.Name] = struct{}{}
	}

	return nil
}

func loadFile(path string) ([]byte, error) {
	if path == "" {
		return nil, nil
	}
	fullPath, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	buf, err := ioutil.ReadFile(filepath.Clean(fullPath))
	if err != nil {
		return nil, err
	}
	return buf, nil
}

func pruneClusters(clusters []*Cluster, nIds, comIds *orderedmap.OrderedMap) {
	for _, c := range clusters {
		filteredNodes := []*Node{}
		for _, n := range c.Nodes {
			_, ok := nIds.Get(n.Id())
			if ok {
				// node component
				filteredComponents := []*Component{}
				for _, com := range n.Components {
					_, ok := comIds.Get(com.Id())
					if ok {
						filteredComponents = append(filteredComponents, com)
					}
				}
				n.Components = filteredComponents

				filteredNodes = append(filteredNodes, n)
			}
		}
		c.Nodes = filteredNodes
		filteredComponents := []*Component{}
		for _, com := range c.Components {
			_, ok := comIds.Get(com.Id())
			if ok {
				filteredComponents = append(filteredComponents, com)
			}
		}
		c.Components = filteredComponents

		pruneClusters(c.Children, nIds, comIds)
	}
}

func (cfg *Config) parseComponent(comName string) (*Component, error) {
	c := &Component{}
	if queryContains(comName) {
		var m ComponentMetadata
		splited := querySplit(comName)
		c.Name = splited[0]
		if err := qs.Unmarshal(&m, splited[1]); err != nil {
			return nil, err
		}
		if m.Icon != "" {
			if _, err := cfg.iconMap.Get(m.Icon); err != nil {
				return nil, fmt.Errorf("not found icon: %s", m.Icon)
			}
			m.IconPath = filepath.Join(cfg.TempIconDir(), fmt.Sprintf("%s.png", m.Icon))
		}
		c.Metadata = m
	} else {
		c.Name = comName
	}
	return c, nil
}

func querySplit(s string) []string {
	splitted := strings.Split(qRep.Replace(s), Q)
	unescaped := []string{}
	for _, ss := range splitted {
		unescaped = append(unescaped, unqRep.Replace(ss))
	}
	return unescaped
}

func queryContains(s string) bool {
	return strings.Contains(qRep.Replace(s), Q)
}

func sepCount(s string) int {
	return strings.Count(escRep.Replace(s), Sep)
}

func sepSplit(s string) []string {
	splitted := strings.Split(escRep.Replace(s), Sep)
	unescaped := []string{}
	for _, ss := range splitted {
		unescaped = append(unescaped, unescRep.Replace(ss))
	}
	return unescaped
}

func sepJoin(ss []string) string {
	return strings.Join(ss, Sep)
}

func sepContains(s string) bool {
	return strings.Contains(escRep.Replace(s), Sep)
}

func layerContains(s []*Layer, e string) bool {
	for _, v := range s {
		if e == v.Name {
			return true
		}
	}
	return false
}

func merge(a, b []string) []string {
	m := orderedmap.NewOrderedMap()
	for _, s := range a {
		m.Set(s, s)
	}
	for _, s := range b {
		m.Set(s, s)
	}
	o := []string{}
	for _, k := range m.Keys() {
		s, _ := m.Get(k)
		o = append(o, s.(string))
	}
	return o
}
