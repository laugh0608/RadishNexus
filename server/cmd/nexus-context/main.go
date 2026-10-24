// nexus-context moves the portable collaboration profile through authenticated
// same-origin APIs. It never accepts a database URL, actor override, or secret
// on the command line.
package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	cp "github.com/laugh0608/RadishNexus/server/internal/contextpackage"
	"github.com/laugh0608/RadishNexus/server/internal/platform/httptransport"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	if e := run(ctx, os.Args[1:], os.Stdout); e != nil {
		fmt.Fprintln(os.Stderr, e)
		os.Exit(1)
	}
}

type options struct{ command, origin, ca, credentials, workspace, project, input, output, operation, plan, confirmed string }

func run(ctx context.Context, args []string, out io.Writer) (err error) {
	if len(args) == 0 {
		return errors.New("usage: nexus-context export|prepare|inspect|apply|result [flags]")
	}
	o := options{command: args[0]}
	f := flag.NewFlagSet("nexus-context", flag.ContinueOnError)
	f.SetOutput(io.Discard)
	for key, ptr := range map[string]*string{"origin": &o.origin, "ca": &o.ca, "credentials-file": &o.credentials, "workspace": &o.workspace, "project": &o.project, "input": &o.input, "output": &o.output, "operation": &o.operation, "plan": &o.plan, "confirm-digest": &o.confirmed} {
		f.StringVar(ptr, key, "", key)
	}
	if e := f.Parse(args[1:]); e != nil || f.NArg() != 0 {
		return errors.New("invalid context arguments; credentials are accepted only via --credentials-file")
	}
	var b cp.Bundle
	if o.command == "prepare" || o.command == "apply" || o.command == "inspect" {
		if o.input == "" {
			return errors.New("--input is required")
		}
		b, err = cp.ReadDirectory(o.input)
		if err != nil {
			return errors.New("package input failed strict validation")
		}
	}
	if o.command == "inspect" {
		d, _, e := cp.Unpack(b)
		if e != nil {
			return e
		}
		return json.NewEncoder(out).Encode(map[string]any{"format": cp.Format, "profile": cp.Profile, "package_digest": cp.Digest(b.Manifest), "objects": len(d.Objects), "history": len(d.History), "document_revisions": len(d.Revisions), "historical_actors": len(d.Actors), "permission_policy": cp.PermissionPolicy, "notice": "Historical facts are unverified package claims. Historical authors cannot log in. Only the importer initially receives access. Text links are preserved without remapping."})
	}
	if !cp.ValidID(o.workspace, "wrk_") {
		return errors.New("valid --workspace is required")
	}
	if o.command == "export" && (!cp.ValidID(o.project, "prj_") || o.output == "") {
		return errors.New("export requires --project and new --output directory")
	}
	if o.command == "prepare" && (!cp.ValidOperation(o.operation) || o.output == "") {
		return errors.New("prepare requires a UUID --operation and new --output plan file")
	}
	if o.command == "result" && !cp.ValidOperation(o.operation) {
		return errors.New("result requires a UUID --operation")
	}
	var plan cp.Plan
	if o.command == "apply" {
		if o.plan == "" || o.confirmed == "" {
			return errors.New("apply requires --plan and --confirm-digest")
		}
		raw, e := readPrivate(o.plan, cp.MaxBytes)
		if e != nil {
			return e
		}
		if e = cp.Decode(raw, &plan); e != nil || plan.PlanDigest != o.confirmed || plan.WorkspaceID != o.workspace || plan.PackageDigest != cp.Digest(b.Manifest) {
			return errors.New("plan, package, Workspace or confirmation digest mismatch")
		}
	}
	if o.command != "export" && o.command != "prepare" && o.command != "apply" && o.command != "result" {
		return errors.New("unknown context command")
	}
	client, e := newClient(o.origin, o.ca)
	if e != nil {
		return e
	}
	credentials, e := readPrivate(o.credentials, 16<<10)
	if e != nil {
		return e
	}
	var login struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if e = cp.Decode(credentials, &login); e != nil || login.Email == "" || login.Password == "" {
		return errors.New("invalid credentials file")
	}
	if e = client.request(ctx, "POST", "/api/v1/auth/sessions", login, nil); e != nil {
		return e
	}
	login.Password = ""
	clear(credentials)
	defer func() {
		logoutCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
		defer cancel()
		if e := client.request(logoutCtx, "DELETE", "/api/v1/auth/session", nil, nil); e != nil {
			err = errors.Join(err, errors.New("operation finished but CLI session logout was not confirmed; revoke it before handoff"))
		}
	}()
	base := "/api/v1/workspaces/" + o.workspace
	switch o.command {
	case "export":
		var result cp.Bundle
		if e = client.request(ctx, "POST", base+"/projects/"+o.project+"/context-export", struct{}{}, &result); e != nil {
			return e
		}
		if e = cp.WriteDirectory(o.output, result); e != nil {
			return errors.New("export output could not be completed; existing output is never overwritten")
		}
		return json.NewEncoder(out).Encode(map[string]string{"status": "exported", "package_digest": cp.Digest(result.Manifest)})
	case "prepare":
		if e = client.request(ctx, "POST", base+"/context-imports/prepare", struct {
			Bundle    cp.Bundle `json:"bundle"`
			Operation string    `json:"operation_id"`
		}{b, o.operation}, &plan); e != nil {
			return e
		}
		if plan.WorkspaceID != o.workspace || plan.OperationID != o.operation || plan.PackageDigest != cp.Digest(b.Manifest) || plan.Policy != cp.PermissionPolicy {
			return errors.New("server returned an inconsistent plan")
		}
		if e = writeNew(o.output, append(cp.Encode(plan), '\n')); e != nil {
			return e
		}
		return printPlan(out, plan)
	case "apply":
		var result cp.Result
		if e = client.request(ctx, "POST", base+"/context-imports/apply", struct {
			Bundle    cp.Bundle `json:"bundle"`
			Plan      cp.Plan   `json:"plan"`
			Confirmed string    `json:"confirmed_digest"`
		}{b, plan, o.confirmed}, &result); e != nil {
			return errors.Join(e, errors.New("apply may have committed; run result with the same --operation before retrying"))
		}
		if result.Status != "applied" || result.PlanDigest != plan.PlanDigest || result.PackageDigest != plan.PackageDigest {
			return errors.New("import response inconsistent; query operation result")
		}
		return printResult(out, result)
	default:
		var result cp.Result
		if e = client.request(ctx, "GET", base+"/context-imports/"+o.operation, nil, &result); e != nil {
			return e
		}
		return printResult(out, result)
	}
}
func printResult(out io.Writer, r cp.Result) error {
	return json.NewEncoder(out).Encode(r)
}
func printPlan(out io.Writer, p cp.Plan) error {
	return json.NewEncoder(out).Encode(map[string]any{"operation_id": p.OperationID, "plan_digest": p.PlanDigest, "package_digest": p.PackageDigest, "target_identity": p.TargetIdentity, "workspace_id": p.WorkspaceID, "objects": p.Objects, "history": p.History, "document_revisions": p.Revisions, "historical_actors": p.HistoricalActors, "permission_policy": p.Policy, "notice": "Review this exact target and package. Apply accepts historical claims, disables historical authors, creates a restricted Project, and grants initial access only to this importing owner. Stop other business write entry points during apply."})
}
func readPrivate(name string, limit int64) ([]byte, error) {
	before, e := os.Lstat(name)
	if e != nil || !before.Mode().IsRegular() || before.Mode().Perm()&0077 != 0 {
		return nil, errors.New("private input must be a regular owner-only file")
	}
	stat, ok := before.Sys().(*syscall.Stat_t)
	if !ok || stat.Nlink != 1 || stat.Uid != uint32(os.Getuid()) {
		return nil, errors.New("private input ownership or link count invalid")
	}
	f, e := os.Open(name)
	if e != nil {
		return nil, errors.New("private input cannot be opened")
	}
	defer f.Close()
	actual, e := f.Stat()
	if e != nil || !actual.Mode().IsRegular() || actual.Mode().Perm()&0077 != 0 || !os.SameFile(before, actual) || actual.Size() > limit {
		return nil, errors.New("private input changed or exceeds limit")
	}
	actualStat, ok := actual.Sys().(*syscall.Stat_t)
	if !ok || actualStat.Nlink != 1 || actualStat.Uid != uint32(os.Getuid()) {
		return nil, errors.New("private input ownership or links changed")
	}
	raw, e := io.ReadAll(io.LimitReader(f, limit+1))
	if e != nil || int64(len(raw)) > limit {
		return nil, errors.New("private input read failed")
	}
	return raw, nil
}
func writeNew(name string, raw []byte) error {
	f, e := os.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if e != nil {
		return errors.New("plan output must be a new writable file")
	}
	_, e = f.Write(raw)
	if e == nil {
		e = f.Sync()
	}
	closeErr := f.Close()
	if e != nil || closeErr != nil {
		return errors.New("plan output incomplete")
	}
	return nil
}

type apiClient struct {
	client *http.Client
	origin *url.URL
}

func newClient(origin, ca string) (*apiClient, error) {
	if _, e := httptransport.NewBrowserSessionPolicy(origin); e != nil {
		return nil, errors.New("--origin must be an exact HTTPS origin")
	}
	u, _ := url.Parse(origin)
	roots, e := x509.SystemCertPool()
	if e != nil {
		return nil, errors.New("system certificate roots unavailable")
	}
	if ca != "" {
		raw, e := os.ReadFile(filepath.Clean(ca))
		if e != nil || !roots.AppendCertsFromPEM(raw) {
			return nil, errors.New("--ca does not contain a readable CA certificate")
		}
	}
	jar, e := cookiejar.New(nil)
	if e != nil {
		return nil, e
	}
	transport := &http.Transport{TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: roots}, ResponseHeaderTimeout: 30 * time.Second}
	return &apiClient{&http.Client{Transport: transport, Jar: jar, Timeout: 5 * time.Minute, CheckRedirect: func(*http.Request, []*http.Request) error { return errors.New("redirect refused") }}, u}, nil
}
func (c *apiClient) request(ctx context.Context, method, path string, body, out any) error {
	var raw []byte
	if body != nil {
		var e error
		raw, e = json.Marshal(body)
		if e != nil {
			return errors.New("request encoding failed")
		}
	}
	request, e := http.NewRequestWithContext(ctx, method, c.origin.String()+path, bytes.NewReader(raw))
	if e != nil {
		return errors.New("request construction failed")
	}
	request.Header.Set("Origin", c.origin.String())
	request.Header.Set("Accept", "application/json")
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	for _, cookie := range c.client.Jar.Cookies(c.origin) {
		if cookie.Name == httptransport.CSRFCookieName {
			request.Header.Set(httptransport.CSRFHeaderName, cookie.Value)
		}
	}
	response, e := c.client.Do(request)
	if e != nil {
		return errors.New("HTTPS request failed; connection or result may be unconfirmed")
	}
	defer response.Body.Close()
	data, e := io.ReadAll(io.LimitReader(response.Body, (96<<20)+1))
	if e != nil || len(data) > 96<<20 {
		return errors.New("response incomplete or exceeds limit")
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("context API refused request (HTTP %d); inspect server request diagnostics without exposing package content", response.StatusCode)
	}
	if out == nil {
		return nil
	}
	var envelope struct {
		Data json.RawMessage `json:"data"`
		Meta json.RawMessage `json:"meta,omitempty"`
	}
	if e = json.Unmarshal(data, &envelope); e != nil || len(envelope.Data) == 0 {
		return errors.New("invalid response envelope")
	}
	if e = cp.Decode(envelope.Data, out); e != nil {
		return errors.New("invalid context response")
	}
	return nil
}
