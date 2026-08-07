package vault

import (
	"fmt"
	"os"
	"path/filepath"

	"silt/backend/parser"
	"silt/backend/types"
)

// exampleTypes seeds a fresh vault with the shipped example note-type pack
// (core entities + structured rituals) so typed notes are useful out of the
// box. Each file is written only when absent (existence-guarded), so
// re-scaffolding or upgrading never overwrites user edits. They are plain
// YAML under <vault>/.system/types/.
//
// Pack philosophy: entities users filter on + a few recurring rituals — not
// a 1:1 mirror of every page template. Daily / Weekly review stay body
// templates only (core `date` already exists).
var exampleTypes = map[string]string{
	"book.yaml": `# Silt note type — a reading note with structured properties.
name: Book
description: Reading notes with author, status, and rating
heroField: title
properties:
  - name: title
    type: text
    required: true
  - name: author
    type: text
  - name: status
    type: select
    options: [todo, reading, done]
    default: todo
  - name: rating
    type: number
    min: 0
    max: 5
  - name: finished
    type: date
`,
	"meeting.yaml": `# Silt note type — a meeting with attendees and decisions.
name: Meeting
description: Meeting notes with attendees, owner, and the decision reached
heroField: topic
properties:
  - name: topic
    type: text
    required: true
  - name: held_on
    type: date
  - name: owner
    type: text
  - name: attendees
    type: multiselect
  - name: decision
    type: text
`,
	"person.yaml": `# Silt note type — a person contact card.
name: Person
description: People you track — category, contact, org, last met
heroField: name
properties:
  - name: name
    type: text
    required: true
  - name: category
    type: select
    options: [work, personal, family, other]
  - name: contact
    type: text
  - name: org
    type: text
  - name: last_met
    type: date
`,
	"project.yaml": `# Silt note type — a project brief with status and outcome.
name: Project
description: Projects with status, area, target date, and outcome
heroField: title
properties:
  - name: title
    type: text
    required: true
  - name: status
    type: select
    options: [active, on_hold, done]
    default: active
  - name: area
    type: select
    options: [work, personal, learning, home]
  - name: target_date
    type: date
  - name: outcome
    type: text
`,
	"decision.yaml": `# Silt note type — a decision log / ADR entry.
name: Decision
description: Decisions with status and date decided
heroField: title
properties:
  - name: title
    type: text
    required: true
  - name: status
    type: select
    options: [proposed, accepted, superseded, deprecated]
  - name: decided_on
    type: date
`,
	"one_on_one.yaml": `# Silt note type — a 1:1 meeting record.
name: One-on-one
description: 1:1s with who, when, and follow-up date
heroField: with
properties:
  - name: with
    type: text
    required: true
  - name: held_on
    type: date
  - name: follow_up
    type: date
`,
	"standup.yaml": `# Silt note type — a project standup / sync.
name: Standup
description: Standups with project, date, and attendees
heroField: project
properties:
  - name: project
    type: text
    required: true
  - name: held_on
    type: date
  - name: attendees
    type: multiselect
`,
	"retrospective.yaml": `# Silt note type — a retrospective session.
name: Retrospective
description: Retros with optional title, date, and participants
heroField: title
properties:
  - name: title
    type: text
  - name: held_on
    type: date
  - name: participants
    type: multiselect
`,
}

// seedExampleTypes writes the example type files into <vault>/.system/types/
// when they do not already exist. Idempotent and best-effort on the directory
// (MkdirAll is a no-op when it exists).
func seedExampleTypes(vaultPath string) error {
	dir := filepath.Join(vaultPath, ".system", "types")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	for name, content := range exampleTypes {
		path := filepath.Join(dir, name)
		if _, err := os.Stat(path); err == nil {
			continue // keep the user's existing file
		}
		if err := parser.WriteFileAtomic(path, []byte(content)); err != nil {
			return err
		}
	}
	return nil
}

// exampleTypeFiles is the deterministic order in which ExampleTypes exposes
// the seed pack, so callers see a stable enumeration regardless of map
// iteration order.
var exampleTypeFiles = []string{
	"book.yaml",
	"meeting.yaml",
	"person.yaml",
	"project.yaml",
	"decision.yaml",
	"one_on_one.yaml",
	"standup.yaml",
	"retrospective.yaml",
}

// exampleTypeDefs parses the seed YAML once at init so RestoreExampleTypes gets
// ready TypeDefs without re-parsing per call. A parse failure here is a
// programmer error (the YAML ships in-binary), so it surfaces as an init panic
// — caught immediately by tests rather than at the first IPC call.
var exampleTypeDefs = func() []*types.TypeDef {
	out := make([]*types.TypeDef, 0, len(exampleTypeFiles))
	for _, name := range exampleTypeFiles {
		td, err := types.ParseTypeBytes([]byte(exampleTypes[name]), name)
		if err != nil {
			panic(fmt.Sprintf("vault: unparsable seed type %s: %v", name, err))
		}
		out = append(out, td)
	}
	return out
}()

// ExampleTypes returns the shipped example note types as parsed TypeDefs, for
// the RestoreExampleTypes IPC. The YAML source is shared with seedExampleTypes
// so scaffold and restore carry identical schemas; scaffold writes the
// hand-authored bytes verbatim, restore re-serializes via types.SaveType
// (canonical form). Returns defensive copies so callers cannot mutate the
// shared seed defs. Order matches exampleTypeFiles.
func ExampleTypes() []*types.TypeDef {
	out := make([]*types.TypeDef, len(exampleTypeDefs))
	for i, td := range exampleTypeDefs {
		cp := *td
		out[i] = &cp
	}
	return out
}
