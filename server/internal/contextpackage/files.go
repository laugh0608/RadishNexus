package contextpackage

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"io/fs"
	"os"
	"path"
	"sort"
	"strings"
	"syscall"
)

var metadataPaths = []string{"actors.jsonl", "history.jsonl", "links.jsonl", "objects.jsonl", "revisions.jsonl", "scope.json"}
var scopeContent = Encode(struct {
	Scope       string   `json:"scope"`
	Excluded    []string `json:"excluded"`
	Permissions string   `json:"permissions"`
	Credentials string   `json:"credentials"`
	TextLinks   string   `json:"text_links"`
}{
	"authorized-collaboration-closure", []string{"unreadable-content-and-relations", "cross-project-and-development-context", "attachments", "source-identity-and-authorization", "source-command-receipts-and-delivery-state"}, PermissionPolicy, "no-credentials-or-active-identity", "text-links-are-preserved-not-remapped"})

func Pack(m Manifest, d Data) (Bundle, error) {
	if _, err := Validate(d); err != nil {
		return Bundle{}, err
	}
	b := Bundle{Manifest: m, Files: []File{}}
	b.Manifest.Format = Format
	b.Manifest.Version = Version
	b.Manifest.Profile = Profile
	b.Manifest.Files = []Descriptor{}
	add := func(name string, content []byte, lines int) {
		b.Files = append(b.Files, File{name, content})
		b.Manifest.Files = append(b.Manifest.Files, Descriptor{name, int64(len(content)), lines, Hash(content)})
	}
	for _, item := range []struct {
		name    string
		records any
	}{{"objects.jsonl", d.Objects}, {"actors.jsonl", d.Actors}, {"links.jsonl", d.Links}, {"history.jsonl", d.History}, {"revisions.jsonl", d.Revisions}} {
		raw, err := json.Marshal(item.records)
		if err != nil {
			return Bundle{}, invalid("record encoding")
		}
		var records []json.RawMessage
		if err = json.Unmarshal(raw, &records); err != nil {
			return Bundle{}, invalid("record encoding")
		}
		var content bytes.Buffer
		for _, r := range records {
			content.Write(r)
			content.WriteByte('\n')
		}
		add(item.name, content.Bytes(), len(records))
	}
	add("scope.json", scopeContent, 0)
	for name, body := range d.Bodies {
		add(name, []byte(body), 0)
	}
	sort.Slice(b.Files, func(i, j int) bool { return b.Files[i].Path < b.Files[j].Path })
	sort.Slice(b.Manifest.Files, func(i, j int) bool { return b.Manifest.Files[i].Path < b.Manifest.Files[j].Path })
	if _, _, err := Unpack(b); err != nil {
		return Bundle{}, err
	}
	return b, nil
}
func Unpack(b Bundle) (Data, *Index, error) {
	d := Data{Objects: []Object{}, Actors: []Actor{}, Links: []Link{}, History: []History{}, Revisions: []Revision{}, Bodies: map[string]string{}}
	m := b.Manifest
	if len(Encode(m)) > MaxLineBytes {
		return d, nil, invalid("manifest size")
	}
	if m.Format != Format || m.Version != Version || m.Profile != Profile || !ValidID(m.PackageID, "pkg_") || !ValidID(m.WorkspaceID, "wrk_") || !ValidID(m.ProjectID, "prj_") || !validTime(m.CreatedAt) || !label(m.SourceVersion, 100) || len(m.Files) < len(metadataPaths) || len(m.Files) > MaxHistory+len(metadataPaths) || len(b.Files) != len(m.Files) {
		return d, nil, invalid("manifest")
	}
	files := map[string][]byte{}
	total := int64(len(Encode(m)))
	for _, f := range b.Files {
		if !allowedPath(f.Path) {
			return d, nil, invalid("file path")
		}
		if _, ok := files[f.Path]; ok {
			return d, nil, invalid("duplicate file")
		}
		files[f.Path] = f.Content
		total += int64(len(f.Content))
		if total > MaxBytes {
			return d, nil, invalid("package size")
		}
	}
	seen := map[string]bool{}
	for _, desc := range m.Files {
		content, ok := files[desc.Path]
		if !ok || seen[desc.Path] || desc.Bytes != int64(len(content)) || desc.SHA256 != Hash(content) || desc.Lines < 0 || desc.Lines > MaxHistory {
			return d, nil, invalid("file descriptor")
		}
		seen[desc.Path] = true
		switch desc.Path {
		case "objects.jsonl":
			if err := readLines(content, desc.Lines, &d.Objects, MaxObjects); err != nil {
				return d, nil, err
			}
		case "actors.jsonl":
			if err := readLines(content, desc.Lines, &d.Actors, MaxObjects); err != nil {
				return d, nil, err
			}
		case "links.jsonl":
			if err := readLines(content, desc.Lines, &d.Links, MaxHistory); err != nil {
				return d, nil, err
			}
		case "history.jsonl":
			if err := readLines(content, desc.Lines, &d.History, MaxHistory); err != nil {
				return d, nil, err
			}
		case "revisions.jsonl":
			if err := readLines(content, desc.Lines, &d.Revisions, MaxHistory); err != nil {
				return d, nil, err
			}
		case "scope.json":
			if !bytes.Equal(content, scopeContent) || desc.Lines != 0 {
				return d, nil, invalid("scope contract")
			}
		default:
			if desc.Lines != 0 {
				return d, nil, invalid("Markdown descriptor")
			}
			d.Bodies[desc.Path] = string(content)
		}
	}
	for _, name := range metadataPaths {
		if !seen[name] {
			return d, nil, invalid("missing file")
		}
	}
	index, err := Validate(d)
	if err != nil {
		return d, nil, err
	}
	if _, ok := index.Objects[Ref{"project", m.ProjectID}]; !ok {
		return d, nil, invalid("manifest Project")
	}
	for _, object := range d.Objects {
		if object.UpdatedAt.After(m.CreatedAt) {
			return d, nil, invalid("object after snapshot")
		}
	}
	for _, link := range d.Links {
		if link.UpdatedAt.After(m.CreatedAt) {
			return d, nil, invalid("relation after snapshot")
		}
	}
	return d, index, nil
}
func readLines[T any](content []byte, expected int, out *[]T, limit int) error {
	if len(content) > 0 && content[len(content)-1] != '\n' {
		return invalid("JSONL termination")
	}
	scan := bufio.NewScanner(bytes.NewReader(content))
	scan.Buffer(make([]byte, 4096), MaxLineBytes+1)
	for scan.Scan() {
		if len(*out) >= limit {
			return invalid("record count")
		}
		var v T
		if len(scan.Bytes()) == 0 || len(scan.Bytes()) > MaxLineBytes {
			return invalid("JSONL record")
		}
		if err := Decode(scan.Bytes(), &v); err != nil {
			return err
		}
		*out = append(*out, v)
	}
	if scan.Err() != nil || len(*out) != expected {
		return invalid("JSONL count/limit")
	}
	return nil
}
func allowedPath(p string) bool {
	for _, name := range metadataPaths {
		if p == name {
			return true
		}
	}
	parts := strings.Split(p, "/")
	if len(parts) != 3 || parts[0] != "documents" || !ValidID(parts[1], "doc_") || !strings.HasSuffix(parts[2], ".md") {
		return false
	}
	value := strings.TrimSuffix(parts[2], ".md")
	if value == "" || value[0] == '0' || len(value) > 5 {
		return false
	}
	for _, c := range value {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}
func readRegular(root *os.Root, name string, limit int64) ([]byte, error) {
	info, err := root.Lstat(name)
	if err != nil || !info.Mode().IsRegular() {
		return nil, invalid("regular file required")
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Nlink != 1 {
		return nil, invalid("linked file")
	}
	file, err := root.Open(name)
	if err != nil {
		return nil, invalid("open file")
	}
	defer file.Close()
	actual, err := file.Stat()
	if err != nil || !actual.Mode().IsRegular() || !os.SameFile(info, actual) || actual.Size() > limit {
		return nil, invalid("changed or oversized file")
	}
	actualStat, ok := actual.Sys().(*syscall.Stat_t)
	if !ok || actualStat.Nlink != 1 {
		return nil, invalid("changed linked file")
	}
	content, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil || int64(len(content)) > limit {
		return nil, invalid("read file")
	}
	return content, nil
}
func ReadDirectory(directory string) (Bundle, error) {
	before, err := os.Lstat(directory)
	if err != nil || !before.IsDir() {
		return Bundle{}, invalid("package directory")
	}
	root, err := os.OpenRoot(directory)
	if err != nil {
		return Bundle{}, invalid("open package directory")
	}
	defer root.Close()
	actual, err := root.Stat(".")
	if err != nil || !os.SameFile(before, actual) {
		return Bundle{}, invalid("changed package directory")
	}
	raw, err := readRegular(root, "manifest.json", MaxLineBytes)
	if err != nil {
		return Bundle{}, err
	}
	var b Bundle
	if err = Decode(raw, &b.Manifest); err != nil {
		return b, err
	}
	if len(b.Manifest.Files) > MaxHistory+len(metadataPaths) {
		return b, invalid("file count")
	}
	allowed := map[string]bool{"manifest.json": true}
	total := int64(len(raw))
	for _, desc := range b.Manifest.Files {
		if !allowedPath(desc.Path) || allowed[desc.Path] || desc.Bytes < 0 || desc.Bytes > MaxBytes {
			return b, invalid("file descriptor")
		}
		allowed[desc.Path] = true
		content, e := readRegular(root, desc.Path, MaxBytes-total)
		if e != nil {
			return b, e
		}
		total += int64(len(content))
		if total > MaxBytes {
			return b, invalid("package size")
		}
		b.Files = append(b.Files, File{desc.Path, content})
	}
	count := 0
	err = fs.WalkDir(root.FS(), ".", func(name string, entry fs.DirEntry, walkErr error) error {
		count++
		if walkErr != nil || count > 2*MaxHistory+32 {
			return invalid("package entries")
		}
		if entry.IsDir() {
			if name == "." || name == "documents" {
				return nil
			}
			parts := strings.Split(name, "/")
			if len(parts) == 2 && parts[0] == "documents" && ValidID(parts[1], "doc_") {
				return nil
			}
			return invalid("unknown directory")
		}
		if !allowed[name] || entry.Type()&os.ModeSymlink != 0 {
			return invalid("unlisted or linked entry")
		}
		return nil
	})
	if err != nil {
		return b, err
	}
	_, _, err = Unpack(b)
	return b, err
}

// WriteDirectory refuses existing destinations. The manifest is written last:
// interrupted output is deliberately unusable, never a successful partial pack.
func WriteDirectory(directory string, b Bundle) error {
	if _, _, err := Unpack(b); err != nil {
		return err
	}
	if err := os.Mkdir(directory, 0700); err != nil {
		return err
	}
	root, err := os.OpenRoot(directory)
	if err != nil {
		return err
	}
	defer root.Close()
	for _, f := range b.Files {
		dir := path.Dir(f.Path)
		if dir != "." {
			if err = root.MkdirAll(dir, 0700); err != nil {
				return err
			}
		}
		if err = writeNew(root, f.Path, f.Content); err != nil {
			return err
		}
	}
	return writeNew(root, "manifest.json", append(Encode(b.Manifest), '\n'))
}
func writeNew(root *os.Root, name string, content []byte) error {
	f, err := root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	_, err = f.Write(content)
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	return closeErr
}
