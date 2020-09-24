package output

import (
	"fmt"
	"io"
	"strings"
	"text/template"

	"github.com/elliotchance/orderedmap"
	"github.com/k1LoW/ndiag/config"
)

type Output interface {
	OutputDiagram(wr io.Writer, d *config.Diagram) error
}

var unescRep = strings.NewReplacer(fmt.Sprintf("%s%s", config.Esc, config.Sep), config.Sep)
var nl2brRep = strings.NewReplacer("\r\n", "<br>", "\n", "<br>", "\r", "<br>")
var crRep = strings.NewReplacer("\r", "")
var clusterRep = strings.NewReplacer(":", "")

var FuncMap = template.FuncMap{
	"trim": func(s string) string {
		return strings.TrimRight(s, "\r\n")
	},
	"nl2br": func(s string) string {
		return nl2brRep.Replace(s)
	},
	"id": func(e config.NNode) string {
		return unescRep.Replace(e.Id())
	},
	"fullname": func(e config.NNode) string {
		return unescRep.Replace(e.FullName())
	},
	"unesc": func(s string) string {
		return unescRep.Replace(s)
	},
	"summary": func(s string) string {
		splitted := strings.Split(crRep.Replace(strings.TrimRight(s, "\r\n")), "\n")
		switch {
		case len(splitted) == 0:
			return ""
		case len(splitted) == 1:
			return splitted[0]
		case len(splitted) == 2 && splitted[1] == "":
			return splitted[0]
		default:
			return fmt.Sprintf("%s ...", splitted[0])
		}
	},
	"imgpath": func(prefix string, vals interface{}, format string) string {
		var strs []string
		switch v := vals.(type) {
		case string:
			strs = []string{v}
		case []string:
			strs = v
		}
		return config.ImagePath(prefix, strs, format)
	},
	"mdpath": func(prefix string, vals interface{}) string {
		var strs []string
		switch v := vals.(type) {
		case string:
			strs = []string{v}
		case []string:
			strs = v
		}
		return config.MdPath(prefix, strs)
	},
	"componentlink": componentLink,
	"nwlink":        nwLink,
	"fromlinks": func(edges []*config.NEdge, base *config.Component) string {
		links := []string{}
		for _, e := range edges {
			if e.Src.Id() != base.Id() {
				links = append(links, componentLink(e.Src))
			}
		}
		return strings.Join(unique(links), " / ")
	},
	"tolinks": func(edges []*config.NEdge, base *config.Component) string {
		links := []string{}
		for _, e := range edges {
			if e.Dst.Id() != base.Id() {
				links = append(links, componentLink(e.Dst))
			}
		}
		return strings.Join(unique(links), " / ")
	},
	"attrs": func(attrs []*config.Attr) string {
		if len(attrs) == 0 {
			return ""
		}
		var out string
		for _, a := range attrs {
			out = fmt.Sprintf("%s, %s=%s", out, a.Key, a.Value)
		}
		return out
	},
}

// componentLink
func componentLink(c *config.Component) string {
	switch {
	case c.Node != nil:
		return fmt.Sprintf("[%s](%s)", c.Id(), config.MdPath("node", []string{c.Node.Id()}))
	case c.Cluster != nil:
		return fmt.Sprintf("[%s](%s#%s)", c.Id(), config.MdPath("layer", []string{c.Cluster.Layer}), clusterRep.Replace(c.Cluster.Id()))
	default:
		return c.Id()
	}
}

func nwLink(nw *config.Network) string {
	cIds := []string{}
	for _, r := range nw.Route {
		cIds = append(cIds, r.FullName())
	}
	return fmt.Sprintf("[%s](%s)", strings.Join(cIds, " -> "), config.MdPath("network", []string{nw.Id()}))
}

func unique(in []string) []string {
	m := orderedmap.NewOrderedMap()
	for _, s := range in {
		m.Set(s, s)
	}
	u := []string{}
	for _, k := range m.Keys() {
		s, _ := m.Get(k)
		u = append(u, s.(string))
	}
	return u
}
