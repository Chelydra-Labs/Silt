package main

import (
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"testing"

	"silt/backend/parser"
)

// undoRestorableField is one exported ParsedBlock field the task undo restores,
// paired with the frontend-facing name it must be reported under.
type undoRestorableField struct {
	field string
	name  string
}

// undoRestorableFields is the authoritative classification of the task state
// the journal restores. TestTaskUndoFieldSetsStayInLockstep drives each entry
// through both changedTaskFields and applyTaskUndoSnapshot, so adding a field
// to only one of them fails the test instead of shipping an undo that restores
// state it does not advertise (or the reverse).
var undoRestorableFields = []undoRestorableField{
	{"CleanText", "text"},
	{"Status", "status"},
	{"CompletedAt", "completed"},
	{"Owner", "owner"},
	{"StartDate", "startDate"},
	{"DueDate", "dueDate"},
	{"Priority", "priority"},
	{"Pinned", "pinned"},
	{"Progress", "progress"},
	{"Recurrence", "recurrence"},
	{"BlockedBy", "blockedBy"},
	{"Estimate", "estimate"},
	{"ManualOrder", "order"},
}

// undoNeverRestoredFields classifies the remaining exported ParsedBlock fields
// so a newly added field cannot silently fall outside the predicate: the test
// fails until it is either restorable or listed here.
var undoNeverRestoredFields = map[string]string{
	"ID":          "identity",
	"ParentID":    "structure",
	"Type":        "structure",
	"Depth":       "structure",
	"RawText":     "rendered from CleanText and type-specific tokens",
	"LineNumber":  "structure",
	"FileDate":    "structure",
	"ModifiedAt":  "re-stamped by every write, including the undo",
	"CreatedAt":   "creation provenance, not a task setter",
	"Author":      "NOTE-only comment attribution, not task state",
	"Timestamp":   "NOTE-only comment attribution, not task state",
	"ExtraTokens": "unknown Dataview tokens round-trip verbatim",
	"Language":    "code-fence info string, not task state",
}

func TestTaskUndoFieldSetsStayInLockstep(t *testing.T) {
	typ := reflect.TypeOf(parser.ParsedBlock{})
	restorable := make(map[string]string, len(undoRestorableFields))
	for _, f := range undoRestorableFields {
		if _, ok := restorable[f.field]; ok {
			t.Fatalf("%s is classified twice", f.field)
		}
		restorable[f.field] = f.name
	}
	for name, why := range undoNeverRestoredFields {
		if _, ok := restorable[name]; ok {
			t.Fatalf("%s is both restorable and never-restored (%s)", name, why)
		}
	}

	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		if field.PkgPath != "" {
			continue // unexported
		}
		wantName, isRestorable := restorable[field.Name]
		if !isRestorable {
			if _, classified := undoNeverRestoredFields[field.Name]; !classified {
				t.Errorf("ParsedBlock.%s is neither restorable by task undo nor listed in undoNeverRestoredFields: classify it", field.Name)
				continue
			}
		}

		var before, after parser.ParsedBlock
		setNonZeroValue(t, reflect.ValueOf(&before).Elem().Field(i))
		got := changedTaskFields([]taskUndoBlock{{BlockID: "b", Before: before, After: after}})
		dst := after
		applyTaskUndoSnapshot(&dst, before, after)

		if !isRestorable {
			if len(got) != 0 {
				t.Errorf("ParsedBlock.%s is not task-restorable but changedTaskFields reported %v", field.Name, got)
			}
			if !reflect.DeepEqual(dst, after) {
				t.Errorf("ParsedBlock.%s is not task-restorable but applyTaskUndoSnapshot restored it", field.Name)
			}
			continue
		}
		if !reflect.DeepEqual(got, []string{wantName}) {
			t.Errorf("changedTaskFields for %s = %v, want [%s]", field.Name, got, wantName)
		}
		if !reflect.DeepEqual(dst, before) {
			t.Errorf("applyTaskUndoSnapshot did not restore %s", field.Name)
		}
	}

	// The TS TaskChangedField union is the third copy of this list; a name
	// added to only one side would be dropped by the frontend's closed set.
	sdkBytes, err := os.ReadFile(filepath.Join("frontend", "src", "plugins", "sdk.ts"))
	if err != nil {
		t.Fatalf("read plugin SDK: %v", err)
	}
	want := make([]string, 0, len(undoRestorableFields))
	for _, f := range undoRestorableFields {
		want = append(want, f.name)
	}
	sort.Strings(want)
	if got := sdkTaskChangedFieldUnion(t, string(sdkBytes)); !reflect.DeepEqual(got, want) {
		t.Errorf("TaskChangedField union = %v, want %v", got, want)
	}
}

// sdkTaskChangedFieldUnion extracts the string literals of the
// TaskChangedField union from the plugin SDK source.
func sdkTaskChangedFieldUnion(t *testing.T, sdk string) []string {
	t.Helper()
	unionRe := regexp.MustCompile(`(?s)export type TaskChangedField =\s*(.*?)(?:\n\s*\n|;)`)
	m := unionRe.FindStringSubmatch(sdk)
	if m == nil {
		t.Fatalf("TaskChangedField union not found in the plugin SDK")
	}
	nameRe := regexp.MustCompile(`'([A-Za-z][A-Za-z0-9]*)'`)
	names := make([]string, 0, len(undoRestorableFields))
	for _, nm := range nameRe.FindAllStringSubmatch(m[1], -1) {
		names = append(names, nm[1])
	}
	sort.Strings(names)
	return names
}

// setNonZeroValue sets a distinguishable, non-zero value on a ParsedBlock
// field so a before/after pair differs in exactly that field.
func setNonZeroValue(t *testing.T, v reflect.Value) {
	t.Helper()
	switch v.Kind() {
	case reflect.String:
		v.SetString("non-zero")
	case reflect.Int:
		v.SetInt(7)
	case reflect.Bool:
		v.SetBool(true)
	case reflect.Ptr:
		p := reflect.New(v.Type().Elem())
		setNonZeroValue(t, p.Elem())
		v.Set(p)
	case reflect.Slice:
		s := reflect.MakeSlice(v.Type(), 1, 1)
		setNonZeroValue(t, s.Index(0))
		v.Set(s)
	default:
		t.Fatalf("no synthetic value for %s (kind %s); extend setNonZeroValue", v.Type(), v.Kind())
	}
}
