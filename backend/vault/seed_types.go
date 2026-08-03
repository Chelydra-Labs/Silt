package vault

import (
	"os"
	"path/filepath"

	"silt/backend/parser"
)

// exampleTypes seeds a fresh vault with two example note types (Book, Meeting)
// so the typed-notes feature is discoverable. Each is written only when
// absent (existence-guarded), so re-scaffolding an existing vault or upgrading
// an in-use vault never overwrites the user's edits. They are plain YAML the
// user can edit or delete freely under <vault>/.system/types/.
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
