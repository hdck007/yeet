package filter

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/hdck007/yeet/internal/ignore"
)

type TreeNode struct {
	Name     string
	IsDir    bool
	Children []*TreeNode
}

type TreeOpts struct {
	MaxDepth        int
	CollapseThreshold int // directories with more files than this show count instead
}

func DefaultTreeOpts() TreeOpts {
	return TreeOpts{
		MaxDepth:        3,
		CollapseThreshold: 10,
	}
}

func BuildTree(root string, opts TreeOpts) (*TreeNode, error) {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}

	info, err := os.Stat(absRoot)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("%s is not a directory", root)
	}

	matcher := ignore.NewMatcher(absRoot)
	rootNode := &TreeNode{Name: filepath.Base(absRoot), IsDir: true}
	buildSubtree(absRoot, rootNode, matcher, opts, 0)
	collapseTree(rootNode)
	return rootNode, nil
}

func buildSubtree(dir string, node *TreeNode, matcher *ignore.Matcher, opts TreeOpts, depth int) {
	if opts.MaxDepth > 0 && depth >= opts.MaxDepth {
		count := countEntries(dir, matcher)
		if count > 0 {
			node.Children = append(node.Children, &TreeNode{
				Name: fmt.Sprintf("(%d entries)", count),
			})
		}
		return
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}

	// Separate dirs and files, sort each
	var dirs, files []fs.DirEntry
	isRoot := depth == 0
	for _, e := range entries {
		if matcher.ShouldIgnoreAt(e.Name(), e.IsDir(), isRoot) {
			continue
		}
		if e.IsDir() {
			dirs = append(dirs, e)
		} else {
			files = append(files, e)
		}
	}

	// If too many files, collapse
	if len(files) > opts.CollapseThreshold {
		node.Children = append(node.Children, &TreeNode{
			Name: fmt.Sprintf("(%d files)", len(files)),
		})
	} else {
		for _, f := range files {
			node.Children = append(node.Children, &TreeNode{Name: f.Name()})
		}
	}

	for _, d := range dirs {
		child := &TreeNode{Name: d.Name(), IsDir: true}
		node.Children = append(node.Children, child)
		buildSubtree(filepath.Join(dir, d.Name()), child, matcher, opts, depth+1)
	}
}

func countEntries(dir string, matcher *ignore.Matcher) int {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0
	}
	count := 0
	for _, e := range entries {
		if !matcher.ShouldIgnore(e.Name(), e.IsDir()) {
			count++
		}
	}
	return count
}

// collapseTree collapses single-child directory chains: a/ -> b/ -> c/ becomes a/b/c/
func collapseTree(node *TreeNode) {
	for _, child := range node.Children {
		collapseTree(child)
	}

	if node.IsDir && len(node.Children) == 1 && node.Children[0].IsDir {
		child := node.Children[0]
		node.Name = node.Name + "/" + child.Name
		node.Children = child.Children
		// Keep collapsing if the merged child also has a single dir child
		collapseTree(node)
	}
}

func RenderTree(w io.Writer, node *TreeNode) {
	fmt.Fprintln(w, node.Name+"/")
	renderChildren(w, node.Children, "")
}

func renderChildren(w io.Writer, children []*TreeNode, prefix string) {
	// Sort: directories first, then files
	sort.Slice(children, func(i, j int) bool {
		if children[i].IsDir != children[j].IsDir {
			return children[i].IsDir
		}
		return strings.ToLower(children[i].Name) < strings.ToLower(children[j].Name)
	})

	for i, child := range children {
		isLast := i == len(children)-1
		connector := "├── "
		if isLast {
			connector = "└── "
		}

		suffix := ""
		if child.IsDir {
			suffix = "/"
		}
		fmt.Fprintf(w, "%s%s%s%s\n", prefix, connector, child.Name, suffix)

		if child.IsDir && len(child.Children) > 0 {
			childPrefix := prefix + "│   "
			if isLast {
				childPrefix = prefix + "    "
			}
			renderChildren(w, child.Children, childPrefix)
		}
	}
}
