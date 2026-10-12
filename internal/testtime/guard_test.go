package testtime

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	pathpkg "path"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// pendingPackages names, by module-relative directory, every package whose tests still carry a
// fixed timing. The guard skips their findings — it logs them, so `go test -v` lists what a
// migration has left — and fails on a pending package that has none left, so the set only
// shrinks. The migration items empty it; a new package never joins it.
var pendingPackages = map[string]bool{
	"cmd/demorig":         true,
	"cmd/stubllm":         true,
	"internal/agent":      true,
	"internal/config":     true,
	"internal/console":    true,
	"internal/domain":     true,
	"internal/filewatch":  true,
	"internal/gitexec":    true,
	"internal/judge":      true,
	"internal/keystore":   true,
	"internal/mcp":        true,
	"internal/mdns":       true,
	"internal/platform":   true,
	"internal/present":    true,
	"internal/provider":   true,
	"internal/reactions":  true,
	"internal/run":        true,
	"internal/schedule":   true,
	"internal/security":   true,
	"internal/stubllm":    true,
	"internal/subprocess": true,
	"internal/tools":      true,
	"internal/undo":       true,
	"internal/update":     true,
	"internal/userexec":   true,
	"internal/workflow":   true,
}

// timedCalls maps every call the guard watches to the index of its duration argument. The
// testtime entries are here because Eventually and Expiry never scale: their argument must
// already be a budgeted value, so a raw literal passed to them is the same fault as a raw sleep.
var timedCalls = map[string]int{
	"time.Sleep":           0,
	"time.After":           0,
	"time.NewTimer":        0,
	"time.AfterFunc":       0,
	"time.NewTicker":       0,
	"time.Tick":            0,
	"context.WithTimeout":  1,
	"context.WithDeadline": 1,
	"tuitest.Within":       0,
	"testtime.Eventually":  1,
	"testtime.Expiry":      0,
}

// timeUnits are the time package's duration constants — the selectors that make a duration fixed.
var timeUnits = map[string]bool{
	"Nanosecond":  true,
	"Microsecond": true,
	"Millisecond": true,
	"Second":      true,
	"Minute":      true,
	"Hour":        true,
}

const (
	// guardDir is this package's own directory, which the walk skips: testtime is where the raw
	// durations are allowed to live.
	guardDir = "internal/testtime"
	// kitDir is the test kit whose non-test files are test code too, and inside which a bare
	// Within is tuitest.Within.
	kitDir = "internal/tuitest"
)

// TestNoFixedTimingInTests is the structural ban on fixed timing in tests (review §4, class A).
// It parses every _test.go file in the module, plus the non-test files of internal/tuitest, and
// reports each timed call whose duration is written raw instead of through testtime.Budget or
// testtime.Fixed, and each hand-rolled wall-clock deadline that should be testtime.Eventually or
// testtime.Expiry. A package in pendingPackages has its findings logged instead of failed.
func TestNoFixedTimingInTests(t *testing.T) {
	t.Parallel()

	byDir := scanModule(t, moduleRoot(t))
	if len(byDir) == 0 {
		t.Fatal("the walk scanned no package — an empty scan means the module root or the file filter is wrong")
	}

	failures, logs := judgeFindings(byDir, pendingPackages)

	for _, line := range logs {
		t.Log(line)
	}
	for _, line := range failures {
		t.Error(line)
	}
}

// TestNoFixedTimingInTestsBites proves the detector on fixtures rather than assuming it: every
// banned form is reported at its line, every allowed form is not, and a pending package's
// findings are logged while another package's fail.
func TestNoFixedTimingInTestsBites(t *testing.T) {
	t.Parallel()

	const fixture = `package sample

import (
	"context"
	"testing"
	"time"

	"github.com/airiclenz/apogee/internal/testtime"
	"github.com/airiclenz/apogee/internal/tuitest"
)

const settle = 50 * time.Millisecond

const (
	first = 3 * time.Second
	second
)

var budgeted = testtime.Budget(5 * time.Second)

func TestForms(t *testing.T) {
	time.Sleep(10 * time.Millisecond)
	<-time.After(time.Second)
	_ = time.NewTimer(2 * time.Second)
	_ = time.AfterFunc(time.Minute, func() {})
	_ = time.NewTicker(100)
	_ = time.Tick(time.Hour)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, stop := context.WithDeadline(ctx, time.Now().Add(time.Second))
	defer stop()
	_ = tuitest.Within(2*settle + tuitest.DefaultTimeout)
	testtime.Eventually(t, 3*time.Second, func() bool { return true }, "a literal")
	_ = testtime.Expiry(time.Second)
	time.Sleep(settle)
	time.Sleep(second)
	wait := settle - time.Since(time.Time{})
	time.Sleep(wait)
	_ = time.Now().Before(time.Time{})
	_ = time.Now().After(time.Time{})
	_ = time.Until(time.Time{})
}

func TestAllowed(t *testing.T, d time.Duration) {
	time.Sleep(testtime.Fixed(10 * time.Millisecond))
	<-time.After(testtime.Budget(time.Second))
	ctx, cancel := context.WithTimeout(context.Background(), tuitest.DefaultTimeout)
	defer cancel()
	_, stop := context.WithDeadline(ctx, time.Now().Add(testtime.Budget(time.Second)))
	defer stop()
	_ = tuitest.Within(budgeted)
	testtime.Eventually(t, testtime.Budget(3*time.Second), func() bool { return true }, "a budget")
	testtime.Eventually(t, budgeted, func() bool { return true }, "a budgeted identifier")
	_ = testtime.Expiry(tuitest.DefaultTimeout)
	remaining := budgeted - time.Since(time.Time{})
	time.Sleep(remaining)
	time.Sleep(d)
	time.Sleep(unknownElsewhere)
	_ = time.Now().Add(time.Second)
}
`
	const kitFixture = `package tuitest

import (
	"time"

	"github.com/airiclenz/apogee/internal/testtime"
)

var DefaultTimeout = testtime.Budget(60 * time.Second)

func waits() {
	_ = Within(5 * time.Second)
	_ = Within(DefaultTimeout)
}
`

	cases := []struct {
		name   string
		path   string
		source string
		inKit  bool
		want   []string
	}{
		{
			name:   "a test package",
			path:   "sample/fixture_test.go",
			source: fixture,
			want: []string{
				"sample/fixture_test.go:22: time.Sleep takes a fixed duration (literal 10) — wrap it in testtime.Budget or testtime.Fixed",
				"sample/fixture_test.go:23: time.After takes a fixed duration (time.Second) — wrap it in testtime.Budget or testtime.Fixed",
				"sample/fixture_test.go:24: time.NewTimer takes a fixed duration (literal 2) — wrap it in testtime.Budget or testtime.Fixed",
				"sample/fixture_test.go:25: time.AfterFunc takes a fixed duration (time.Minute) — wrap it in testtime.Budget or testtime.Fixed",
				"sample/fixture_test.go:26: time.NewTicker takes a fixed duration (literal 100) — wrap it in testtime.Budget or testtime.Fixed",
				"sample/fixture_test.go:27: time.Tick takes a fixed duration (time.Hour) — wrap it in testtime.Budget or testtime.Fixed",
				"sample/fixture_test.go:28: context.WithTimeout takes a fixed duration (literal 5) — wrap it in testtime.Budget or testtime.Fixed",
				"sample/fixture_test.go:30: context.WithDeadline takes a fixed duration (time.Second) — wrap it in testtime.Budget or testtime.Fixed",
				"sample/fixture_test.go:32: tuitest.Within takes a fixed duration (literal 2) — wrap it in testtime.Budget or testtime.Fixed",
				"sample/fixture_test.go:33: testtime.Eventually takes a fixed duration (literal 3) — wrap it in testtime.Budget or testtime.Fixed",
				"sample/fixture_test.go:34: testtime.Expiry takes a fixed duration (time.Second) — wrap it in testtime.Budget or testtime.Fixed",
				"sample/fixture_test.go:35: time.Sleep takes a fixed duration (settle → literal 50) — wrap it in testtime.Budget or testtime.Fixed",
				"sample/fixture_test.go:36: time.Sleep takes a fixed duration (second → literal 3) — wrap it in testtime.Budget or testtime.Fixed",
				"sample/fixture_test.go:38: time.Sleep takes a fixed duration (wait → settle → literal 50) — wrap it in testtime.Budget or testtime.Fixed",
				"sample/fixture_test.go:39: time.Now().Before is a hand-rolled deadline — poll with testtime.Eventually or testtime.Expiry",
				"sample/fixture_test.go:40: time.Now().After is a hand-rolled deadline — poll with testtime.Eventually or testtime.Expiry",
				"sample/fixture_test.go:41: time.Until is a hand-rolled deadline — poll with testtime.Eventually or testtime.Expiry",
			},
		},
		{
			name:   "the kit's own bare Within",
			path:   "internal/tuitest/wait.go",
			source: kitFixture,
			inKit:  true,
			want: []string{
				"internal/tuitest/wait.go:12: Within takes a fixed duration (literal 5) — wrap it in testtime.Budget or testtime.Fixed",
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			fset := token.NewFileSet()
			file, err := parser.ParseFile(fset, tc.path, tc.source, parser.SkipObjectResolution)
			if err != nil {
				t.Fatalf("parse fixture: %v", err)
			}

			got := timingFindings(fset, file, packageInitialisers([]*ast.File{file})[file.Name.Name], tc.inKit)

			if strings.Join(got, "\n") != strings.Join(tc.want, "\n") {
				t.Errorf("detector findings differ\n got:\n  %s\nwant:\n  %s",
					strings.Join(got, "\n  "), strings.Join(tc.want, "\n  "))
			}
		})
	}

	t.Run("pending packages log, others fail, a clean pending package fails", func(t *testing.T) {
		t.Parallel()

		byDir := map[string][]string{
			"a/pending": {"a/pending/x_test.go:3: finding"},
			"b/live":    {"b/live/y_test.go:4: finding"},
			"c/clean":   nil,
		}
		pending := map[string]bool{"a/pending": true, "c/clean": true}

		failures, logs := judgeFindings(byDir, pending)

		wantFailures := []string{
			"b/live/y_test.go:4: finding",
			`c/clean is in pendingPackages but has no findings — remove it from pendingPackages`,
		}
		wantLogs := []string{"a/pending/x_test.go:3: finding"}
		if strings.Join(failures, "\n") != strings.Join(wantFailures, "\n") {
			t.Errorf("failures differ\n got:\n  %s\nwant:\n  %s", strings.Join(failures, "\n  "), strings.Join(wantFailures, "\n  "))
		}
		if strings.Join(logs, "\n") != strings.Join(wantLogs, "\n") {
			t.Errorf("logs differ\n got:\n  %s\nwant:\n  %s", strings.Join(logs, "\n  "), strings.Join(wantLogs, "\n  "))
		}
	})
}

// judgeFindings splits the findings into those that fail the guard and those it only logs. A
// pending package's findings are logged; every other package's fail; a pending package with no
// findings is a failure of its own, so the set cannot outlive the migration it tracks. Both lists
// come out sorted by directory, in each directory's source order.
func judgeFindings(byDir map[string][]string, pending map[string]bool) (failures, logs []string) {
	dirs := make([]string, 0, len(byDir)+len(pending))
	for dir := range byDir {
		dirs = append(dirs, dir)
	}
	for dir := range pending {
		if _, scanned := byDir[dir]; !scanned {
			dirs = append(dirs, dir)
		}
	}
	sort.Strings(dirs)

	for _, dir := range dirs {
		findings := byDir[dir]
		switch {
		case pending[dir] && len(findings) == 0:
			failures = append(failures, dir+" is in pendingPackages but has no findings — remove it from pendingPackages")
		case pending[dir]:
			logs = append(logs, findings...)
		default:
			failures = append(failures, findings...)
		}
	}
	return failures, logs
}

// moduleRoot walks up from the package directory to the directory holding go.mod.
func moduleRoot(t *testing.T) string {
	t.Helper()

	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("no go.mod above the package directory")
		}
		dir = parent
	}
}

// scanModule parses every package directory under root that holds a file the guard covers, and
// returns its findings keyed by module-relative, forward-slash directory — every covered directory
// present, with or without findings. Directories the go tool ignores (dot- or underscore-prefixed,
// testdata, vendor), nested modules and this package are skipped.
func scanModule(t *testing.T, root string) map[string][]string {
	t.Helper()

	byDir := map[string][]string{}
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if !entry.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if path != root && skippedDir(path, entry.Name()) {
			return filepath.SkipDir
		}
		if rel == guardDir {
			return nil
		}
		findings, covered, err := scanDir(path, rel)
		if err != nil {
			return err
		}
		if covered {
			byDir[rel] = findings
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk the module: %v", err)
	}
	return byDir
}

// skippedDir reports whether the walk leaves a directory out: the ones the go tool ignores, and a
// nested module, which is not this module's code.
func skippedDir(path, name string) bool {
	if strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_") || name == "testdata" || name == "vendor" {
		return true
	}
	_, err := os.Stat(filepath.Join(path, "go.mod"))
	return err == nil
}

// scanDir parses one directory's Go files and returns the findings in the files the guard covers,
// and whether there was any such file. Every Go file is parsed, covered or not, so a test's
// identifier resolves to a constant its package declares in a non-test file.
func scanDir(path, rel string) ([]string, bool, error) {
	entries, err := os.ReadDir(path)
	if err != nil {
		return nil, false, err
	}
	inKit := rel == kitDir

	fset := token.NewFileSet()
	var files, covered []*ast.File
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || filepath.Ext(name) != ".go" {
			continue
		}
		src, err := os.ReadFile(filepath.Join(path, name))
		if err != nil {
			return nil, false, err
		}
		// The file is parsed under its module-relative, forward-slash path, so a finding's
		// file:line reads the same on every OS and in any checkout.
		file, err := parser.ParseFile(fset, pathpkg.Join(rel, name), src, parser.SkipObjectResolution)
		if err != nil {
			return nil, false, err
		}
		files = append(files, file)
		if strings.HasSuffix(name, "_test.go") || inKit {
			covered = append(covered, file)
		}
	}
	if len(covered) == 0 {
		return nil, false, nil
	}

	initialisers := packageInitialisers(files)
	var findings []string
	for _, file := range covered {
		findings = append(findings, timingFindings(fset, file, initialisers[file.Name.Name], inKit)...)
	}
	return findings, true, nil
}

// packageInitialisers collects every package-level const and var initialiser across files, keyed
// by package name and then identifier — a test file of package foo_test resolves only against the
// foo_test files. A const spec without values repeats the previous spec's expression, as Go does.
// One name can carry several initialisers (a const declared per build target); each is checked.
func packageInitialisers(files []*ast.File) map[string]map[string][]ast.Expr {
	byPackage := map[string]map[string][]ast.Expr{}
	for _, file := range files {
		initialisers := byPackage[file.Name.Name]
		if initialisers == nil {
			initialisers = map[string][]ast.Expr{}
			byPackage[file.Name.Name] = initialisers
		}
		for _, decl := range file.Decls {
			gen, isGen := decl.(*ast.GenDecl)
			if !isGen || (gen.Tok != token.CONST && gen.Tok != token.VAR) {
				continue
			}
			var previous []ast.Expr
			for _, spec := range gen.Specs {
				valueSpec := spec.(*ast.ValueSpec)
				values := valueSpec.Values
				if len(values) == 0 && gen.Tok == token.CONST {
					values = previous
				}
				previous = values
				for i, name := range valueSpec.Names {
					if value := pairedValue(values, len(valueSpec.Names), i); value != nil {
						initialisers[name.Name] = append(initialisers[name.Name], value)
					}
				}
			}
		}
	}
	return byPackage
}

// timingScan carries what one file's scan needs to judge a duration: where positions resolve,
// the package's own initialisers, and whether the file is the kit's (where a bare Within counts).
type timingScan struct {
	fset         *token.FileSet
	initialisers map[string][]ast.Expr
	inKit        bool
}

// timingFindings reports, in source order, every rule violation in file: (a) a timed call whose
// duration argument holds a time unit or a numeric literal outside testtime.Budget/Fixed —
// resolving the function's own locals and the package's consts and vars to their initialisers —
// and (b) a hand-rolled wall-clock deadline. Findings read `file:line: …`.
func timingFindings(fset *token.FileSet, file *ast.File, initialisers map[string][]ast.Expr, inKit bool) []string {
	scan := timingScan{fset: fset, initialisers: initialisers, inKit: inKit}

	var findings []string
	for _, decl := range file.Decls {
		var body *ast.BlockStmt
		var params *ast.FieldList
		if fn, isFunc := decl.(*ast.FuncDecl); isFunc {
			body, params = fn.Body, fn.Type.Params
		}
		ast.Inspect(decl, func(node ast.Node) bool {
			call, isCall := node.(*ast.CallExpr)
			if !isCall {
				return true
			}
			if finding := scan.check(call, body, params); finding != "" {
				pos := fset.Position(call.Pos())
				findings = append(findings, fmt.Sprintf("%s:%d: %s", pos.Filename, pos.Line, finding))
			}
			return true
		})
	}
	return findings
}

// check judges one call against both rules and returns the finding's text, or "" when it passes.
func (s timingScan) check(call *ast.CallExpr, body *ast.BlockStmt, params *ast.FieldList) string {
	if name := wallClockDeadline(call); name != "" {
		return name + " is a hand-rolled deadline — poll with testtime.Eventually or testtime.Expiry"
	}
	name, index, isTimed := s.timedCall(call)
	if !isTimed || index >= len(call.Args) {
		return ""
	}
	if fixed := s.fixedPart(call.Args[index], body, params, map[string]bool{}); fixed != "" {
		return name + " takes a fixed duration (" + fixed + ") — wrap it in testtime.Budget or testtime.Fixed"
	}
	return ""
}

// wallClockDeadline names a call that rule (b) bans — time.Now().Before(…), time.Now().After(…)
// or time.Until(…) — or returns "".
func wallClockDeadline(call *ast.CallExpr) string {
	if isPackageCall(call, "time", "Until") {
		return "time.Until"
	}
	sel, isSel := call.Fun.(*ast.SelectorExpr)
	if !isSel || (sel.Sel.Name != "Before" && sel.Sel.Name != "After") {
		return ""
	}
	if inner, isCall := sel.X.(*ast.CallExpr); isCall && isPackageCall(inner, "time", "Now") {
		return "time.Now()." + sel.Sel.Name
	}
	return ""
}

// timedCall recognises a call rule (a) watches and returns its name as written and the index of
// its duration argument.
func (s timingScan) timedCall(call *ast.CallExpr) (string, int, bool) {
	switch fun := call.Fun.(type) {
	case *ast.SelectorExpr:
		pkg, isIdent := fun.X.(*ast.Ident)
		if !isIdent {
			return "", 0, false
		}
		name := pkg.Name + "." + fun.Sel.Name
		index, isTimed := timedCalls[name]
		return name, index, isTimed
	case *ast.Ident:
		if s.inKit && fun.Name == "Within" {
			return fun.Name, timedCalls["tuitest.Within"], true
		}
	}
	return "", 0, false
}

// fixedPart returns a description of the first fixed timing inside expr — a time unit, a numeric
// literal, or an identifier whose initialiser holds one, as a `name → … → token` chain — or ""
// when every part is budgeted, fixed by wrapper, or out of reach. A testtime.Budget or
// testtime.Fixed call is opaque: whatever it wraps is accepted. Another package's selector
// (tuitest.DefaultTimeout, a field) is accepted, as is an identifier with no initialiser in reach
// (a parameter, a range variable, a multi-value assignment). Of a call, only the arguments are
// judged: the function it names is not a duration.
func (s timingScan) fixedPart(expr ast.Expr, body *ast.BlockStmt, params *ast.FieldList, seen map[string]bool) string {
	var fixed string
	ast.Inspect(expr, func(node ast.Node) bool {
		if fixed != "" {
			return false
		}
		switch n := node.(type) {
		case *ast.CallExpr:
			if isPackageCall(n, "testtime", "Budget") || isPackageCall(n, "testtime", "Fixed") {
				return false
			}
			for _, arg := range n.Args {
				if fixed = s.fixedPart(arg, body, params, seen); fixed != "" {
					break
				}
			}
			return false
		case *ast.SelectorExpr:
			if pkg, isIdent := n.X.(*ast.Ident); isIdent && pkg.Name == "time" && timeUnits[n.Sel.Name] {
				fixed = "time." + n.Sel.Name
			}
			return false
		case *ast.BasicLit:
			if n.Kind == token.INT || n.Kind == token.FLOAT {
				fixed = "literal " + n.Value
			}
			return false
		case *ast.Ident:
			if inner := s.resolve(n, body, params, seen); inner != "" {
				fixed = n.Name + " → " + inner
			}
			return false
		case *ast.FuncLit:
			return false
		}
		return true
	})
	return fixed
}

// resolve follows an identifier to its initialiser and judges that. A local of the enclosing
// function — the nearest `:=` or `var` declaring it before the use — wins; a parameter or a local
// without an initialiser is accepted; otherwise the package's own consts and vars are tried. seen
// breaks a cycle (`wait := wait * 2`).
func (s timingScan) resolve(ident *ast.Ident, body *ast.BlockStmt, params *ast.FieldList, seen map[string]bool) string {
	if seen[ident.Name] {
		return ""
	}
	seen[ident.Name] = true
	defer delete(seen, ident.Name)

	if init, isLocal := localInitialiser(ident, body, params); isLocal {
		if init == nil {
			return ""
		}
		return s.fixedPart(init, body, params, seen)
	}
	for _, init := range s.initialisers[ident.Name] {
		if fixed := s.fixedPart(init, nil, nil, seen); fixed != "" {
			return fixed
		}
	}
	return ""
}

// localInitialiser finds the declaration of ident's name within the enclosing function that sits
// nearest before the use, and returns its initialiser (nil when it has none in reach) and whether
// the name is a local at all. A parameter is a local without an initialiser.
func localInitialiser(ident *ast.Ident, body *ast.BlockStmt, params *ast.FieldList) (ast.Expr, bool) {
	if body == nil {
		return nil, false
	}
	var (
		init    ast.Expr
		isLocal bool
		nearest token.Pos
	)
	declare := func(name *ast.Ident, value ast.Expr) {
		if name.Name != ident.Name || name.Pos() >= ident.Pos() || name.Pos() < nearest {
			return
		}
		init, isLocal, nearest = value, true, name.Pos()
	}
	if params != nil {
		for _, field := range params.List {
			for _, name := range field.Names {
				declare(name, nil)
			}
		}
	}
	ast.Inspect(body, func(node ast.Node) bool {
		switch n := node.(type) {
		case *ast.AssignStmt:
			if n.Tok != token.DEFINE {
				return true
			}
			for i, lhs := range n.Lhs {
				if name, isIdent := lhs.(*ast.Ident); isIdent {
					declare(name, pairedValue(n.Rhs, len(n.Lhs), i))
				}
			}
		case *ast.ValueSpec:
			for i, name := range n.Names {
				declare(name, pairedValue(n.Values, len(n.Names), i))
			}
		case *ast.RangeStmt:
			for _, expr := range []ast.Expr{n.Key, n.Value} {
				if name, isIdent := expr.(*ast.Ident); isIdent && n.Tok == token.DEFINE {
					declare(name, nil)
				}
			}
		case *ast.FuncLit:
			for _, field := range n.Type.Params.List {
				for _, name := range field.Names {
					declare(name, nil)
				}
			}
		}
		return true
	})
	return init, isLocal
}

// pairedValue returns the value assigned to the i-th of count names, or nil when the values do not
// pair one-to-one with the names (`a, b := f()`, `var x T`).
func pairedValue(values []ast.Expr, count, i int) ast.Expr {
	if len(values) != count {
		return nil
	}
	return values[i]
}

// isPackageCall reports whether call is `pkg.name(…)`.
func isPackageCall(call *ast.CallExpr, pkg, name string) bool {
	sel, isSel := call.Fun.(*ast.SelectorExpr)
	if !isSel || sel.Sel.Name != name {
		return false
	}
	ident, isIdent := sel.X.(*ast.Ident)
	return isIdent && ident.Name == pkg
}
