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
	if m.Format != Format || m.Version != Version || (m.Profile != Profile && m.Profile != ProfileV1 && m.Profile != ProfileV2 && m.Profile != ProfileV3 && m.Profile != ProfileV4) || !ValidID(m.PackageID, "pkg_") || !ValidID(m.WorkspaceID, "wrk_") || !ValidID(m.ProjectID, "prj_") || !validTime(m.CreatedAt) || !label(m.SourceVersion, 100) || len(m.Files) < len(metadataPaths) || len(m.Files) > MaxHistory+len(metadataPaths) || len(b.Files) != len(m.Files) {
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
			var lines []json.RawMessage
			if err := readLines(content, desc.Lines, &lines, MaxHistory); err != nil {
				return d, nil, err
			}
			for _, raw := range lines {
				var fields map[string]json.RawMessage
				count := 16
				if m.Profile != Profile {
					count = 15
				}
				if Decode(raw, &fields) != nil || len(fields) != count || (m.Profile == Profile && fields["supersession_reason"] == nil) || (m.Profile != Profile && fields["supersession_reason"] != nil) {
					return d, nil, invalid("Link profile fields")
				}
				var l Link
				if Decode(raw, &l) != nil || (m.Profile != Profile && l.Relation == "supersedes") {
					return d, nil, invalid("legacy Link")
				}
				d.Links = append(d.Links, l)
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
	if m.Profile != Profile && m.Profile != ProfileV4 {
		for i, o := range d.Objects {
			if o.Type != "decision" {
				continue
			}
			var fields map[string]json.RawMessage
			if Decode(o.Data, &fields) != nil || len(fields) != 10 || fields["rejection_reason"] != nil {
				return d, nil, invalid("legacy Decision fields")
			}
			var status string
			if Decode(fields["status"], &status) != nil || (status != "proposed" && status != "accepted") {
				return d, nil, invalid("legacy Decision fields")
			}
			fields["rejection_reason"] = json.RawMessage("null")
			d.Objects[i].Data = Encode(fields)
		}
		for _, h := range d.History {
			if h.Type == "decision.rejected" {
				return d, nil, invalid("legacy rejection history")
			}
		}
	}
	if m.Profile != Profile {
		for i, o := range d.Objects {
			if o.Type != "decision" {
				continue
			}
			var fields map[string]json.RawMessage
			if Decode(o.Data, &fields) != nil || len(fields) != 11 || fields["decision_revision"] != nil {
				return d, nil, invalid("legacy Decision revision")
			}
			var status string
			if Decode(fields["status"], &status) != nil || (status != "proposed" && status != "accepted" && status != "rejected") {
				return d, nil, invalid("legacy Decision status")
			}
			rev := 2
			if status == "proposed" {
				rev = 1
			}
			fields["decision_revision"] = Encode(rev)
			d.Objects[i].Data = Encode(fields)
		}
		for _, h := range d.History {
			if h.Type == "decision.superseded" {
				return d, nil, invalid("legacy supersession history")
			}
		}
	}

	if m.Profile == ProfileV1 || m.Profile == ProfileV2 {
		for i, o := range d.Objects {
			if o.Type != "ticket" {
				continue
			}
			var fields map[string]json.RawMessage
			expected := []string{"title", "status", "status_revision"}
			if m.Profile == ProfileV2 {
				expected = append(expected, "assignee_user_id", "assignment_revision")
			}
			if Decode(o.Data, &fields) != nil || len(fields) != len(expected) {
				return d, nil, invalid("legacy Ticket fields")
			}
			for _, key := range expected {
				if fields[key] == nil {
					return d, nil, invalid("legacy Ticket fields")
				}
			}
			var t Ticket
			if Decode(o.Data, &t) != nil {
				return d, nil, invalid("legacy Ticket")
			}
			if m.Profile == ProfileV1 {
				t.AssignmentRevision = 1
			}
			t.Priority = "unset"
			t.PriorityRevision = 1
			d.Objects[i].Data = Encode(t)
		}
		for _, h := range d.History {
			if h.Type == "ticket.priority-changed" || (m.Profile == ProfileV1 && h.Type == "ticket.assignee-changed") {
				return d, nil, invalid("unsupported legacy Ticket history")
			}
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
	if err != nil {
		return nil, invalid("regular file required")
	}
	return readRegularSnapshot(root, name, limit, info)
}

// Bind the open descriptor to the inspected inode. A nonblocking open prevents
// a substituted FIFO from waiting for a writer; recheck the path after opening.
func readRegularSnapshot(root *os.Root, name string, limit int64, info os.FileInfo) ([]byte, error) {
	if !info.Mode().IsRegular() {
		return nil, invalid("regular file required")
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Nlink != 1 {
		return nil, invalid("linked file")
	}
	file, err := root.OpenFile(name, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, invalid("open file")
	}
	defer file.Close()
	actual, err := file.Stat()
	if err != nil || !actual.Mode().IsRegular() || !os.SameFile(info, actual) || actual.Size() > limit {
		return nil, invalid("changed or oversized file")
	}
	current, err := root.Lstat(name)
	if err != nil || !current.Mode().IsRegular() || !os.SameFile(actual, current) {
		return nil, invalid("changed file path")
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
	// Keep the on-disk manifest equal to the bytes counted by Unpack.
	return writeNew(root, "manifest.json", Encode(b.Manifest))
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
