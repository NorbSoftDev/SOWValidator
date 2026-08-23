// Package plist reads the packed-sprite dictionaries (.plist) that pair with
// each packed .dds atlas, and reproduces the engine's key -> (pack, frame)
// split the engine performs when it loads them.
package plist

import (
	"encoding/xml"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Pack is one logical sprite pack: a name prefix plus the set of frame indices
// that actually exist for it. Frames are 0-based, matching the engine, which
// subtracts 1 from the 1-based number embedded in the key.
type Pack struct {
	Name    string
	Frames  map[int]string // frame index -> plist file that defined it
	Sources map[string]int // plist file -> number of frames contributed
}

// Max returns the highest frame index present, or -1 when empty.
func (p *Pack) Max() int {
	max := -1
	for f := range p.Frames {
		if f > max {
			max = f
		}
	}
	return max
}

// Min returns the lowest frame index present, or -1 when empty.
func (p *Pack) Min() int {
	min := -1
	for f := range p.Frames {
		if min == -1 || f < min {
			min = f
		}
	}
	return min
}

// Files returns the .plist files that contributed frames to this pack, sorted.
func (p *Pack) Files() []string {
	out := make([]string, 0, len(p.Sources))
	for f := range p.Sources {
		out = append(out, f)
	}
	sort.Strings(out)
	return out
}

// Missing lists frame indices in [begin, begin+count) that are absent.
func (p *Pack) Missing(begin, count int) []int {
	var out []int
	for f := begin; f < begin+count; f++ {
		if _, ok := p.Frames[f]; !ok {
			out = append(out, f)
		}
	}
	return out
}

// Gaps lists holes below Max() -- frames the engine cannot detect, because
// the engine sizes its frame table with zero values and only marks the keys
// it actually sees. A hole therefore keeps index 0,
// a valid-looking texture slot, so the "frame not found" guard
// never fires for it.
func (p *Pack) Gaps() []int {
	var out []int
	for f := 0; f <= p.Max(); f++ {
		if _, ok := p.Frames[f]; !ok {
			out = append(out, f)
		}
	}
	return out
}

// Index is every pack discovered across a set of .plist files.
type Index struct {
	Packs map[string]*Pack
	Files []string
}

func NewIndex() *Index {
	return &Index{Packs: map[string]*Pack{}}
}

func (ix *Index) Get(name string) (*Pack, bool) {
	p, ok := ix.Packs[strings.ToUpper(name)]
	return p, ok
}

// Names returns pack names sorted, for stable output.
func (ix *Index) Names() []string {
	out := make([]string, 0, len(ix.Packs))
	for n := range ix.Packs {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// SplitKey reproduces the engine's key split exactly:
//
//	spack = tmp.Left( tmp.size() - 8 );
//	frame = atoi( tmp.Mid( tmp.size() - 8 ) ) - 1;
//
// The last 8 characters are assumed to be NNNN.png, so "UO01D0001.png" yields
// pack "UO01D" and frame 0. Keys shorter than 9 characters would make the
// engine's Left() call go negative, so we reject them.
func SplitKey(key string) (pack string, frame int, err error) {
	if len(key) < 9 {
		return "", 0, fmt.Errorf("key %q is too short to split (engine needs >= 9 chars)", key)
	}
	cut := len(key) - 8
	pack = strings.ToUpper(key[:cut])
	rest := key[cut:]
	n := atoiPrefix(rest)
	if n == 0 {
		return pack, 0, fmt.Errorf("key %q has no frame number in its last 8 chars (%q)", key, rest)
	}
	return pack, n - 1, nil
}

func atoiPrefix(s string) int {
	i := 0
	for i < len(s) && s[i] == '0' {
		i++
	}
	start := i
	for i < len(s) && s[i] >= '0' && s[i] <= '9' {
		i++
	}
	if start == i {
		// All zeros is a legitimate 0.
		if start > 0 {
			return 0
		}
		return 0
	}
	n := 0
	for _, c := range s[start:i] {
		n = n*10 + int(c-'0')
	}
	return n
}

// BadKey is a key that could not be split into pack + frame.
type BadKey struct {
	File string
	Key  string
	Err  error
}

// LoadDir parses every .plist under dir (recursively) into ix.
func (ix *Index) LoadDir(dir string) ([]BadKey, error) {
	var bad []BadKey
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil // unreadable subtree: skip rather than abort the run
		}
		if d.IsDir() || !strings.EqualFold(filepath.Ext(path), ".plist") {
			return nil
		}
		b, perr := ix.LoadFile(path)
		bad = append(bad, b...)
		if perr != nil {
			bad = append(bad, BadKey{File: path, Err: perr})
		}
		return nil
	})
	return bad, err
}

// LoadFile parses one .plist and folds its frame keys into the index.
func (ix *Index) LoadFile(path string) ([]BadKey, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	ix.Files = append(ix.Files, path)

	keys, err := frameKeys(f)
	if err != nil {
		return nil, err
	}

	var bad []BadKey
	for _, k := range keys {
		name, frame, kerr := SplitKey(k)
		if kerr != nil {
			bad = append(bad, BadKey{File: path, Key: k, Err: kerr})
			continue
		}
		p := ix.Packs[name]
		if p == nil {
			p = &Pack{Name: name, Frames: map[int]string{}, Sources: map[string]int{}}
			ix.Packs[name] = p
		}
		p.Frames[frame] = path
		p.Sources[path]++
	}
	return bad, nil
}

// frameKeys walks the plist XML and returns the keys directly inside the
// top-level "frames" dict -- i.e. the sprite filenames, not the per-frame
// attribute keys nested one level deeper.
func frameKeys(r io.Reader) ([]string, error) {
	dec := xml.NewDecoder(r)
	dec.Strict = false

	var (
		keys        []string
		depth       int
		framesDepth = -1
		expectDict  bool
	)

	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "dict":
				depth++
				if expectDict {
					framesDepth = depth
					expectDict = false
				}
			case "key":
				var s string
				if err := dec.DecodeElement(&s, &t); err != nil {
					return nil, err
				}
				s = strings.TrimSpace(s)
				if framesDepth == -1 {
					if s == "frames" {
						expectDict = true
					}
				} else if depth == framesDepth {
					keys = append(keys, s)
				}
			}
		case xml.EndElement:
			if t.Name.Local == "dict" {
				if depth == framesDepth {
					framesDepth = -1
				}
				depth--
			}
		}
	}
	return keys, nil
}
