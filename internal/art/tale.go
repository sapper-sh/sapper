package art

// Panels in assets/tale are original art drawn by scripts/taleart.py.

import (
	"embed"
	"strings"
)

//go:embed assets/tale
var taleFS embed.FS

// Chapter is one page of `sapper tale`: a picture and a few lines of story.
type Chapter struct {
	Title string
	Art   string // asset name without extension
	Text  []string
}

// Tale is sapper's purpose told as a children's story. Lines are wrapped by hand to
// stay well inside 78 columns.
var Tale = []Chapter{
	{"I. The treasure box", "chest", []string{
		"Imagine you have a treasure box full of your favourite drawings",
		"and photos. It is the most precious thing you own.",
	}},
	{"II. The castle", "castle", []string{
		"sapper builds a strong castle to keep that box safe. Every night",
		"it makes a secret copy of the box and hides it somewhere else.",
	}},
	{"III. The dragon", "dragon", []string{
		"Lots of people make copies and never check them. So every week",
		"sapper pretends a dragon smashed the castle. It rebuilds",
		"everything from the secret copy and checks every single drawing.",
		"Then it says SURVIVED, or BREACHED and how to fix it.",
		"",
		"It even notices when a sneaky monster tries to scribble over all",
		"your drawings at once, and keeps the old copy safe.",
	}},
	{"IV. The warden", "wizard", []string{
		"sapper is the warden who summons a dragon every week and raises a ward,",
		"so it is ready when a real one comes.",
	}},
}

// TaleArt returns the lines of a tale picture, plain ASCII when ascii is true.
func TaleArt(name string, ascii bool) []string {
	ext := ".txt"
	if ascii {
		ext = ".ascii"
	}
	b, err := taleFS.ReadFile("assets/tale/" + name + ext)
	if err != nil {
		return nil
	}
	return strings.Split(strings.TrimRight(string(b), "\n"), "\n")
}
