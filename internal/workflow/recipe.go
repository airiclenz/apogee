package workflow

import "io/fs"

// Recipe is one human-written Recipe as the engine starts it (ADR 0087 D6): the stage list a
// skill's header declares, the inputs the user's text binds, and where the skill's folder is. It
// is the value the RecipeSource port hands the agent, so the loop can start a recipe without
// importing the skills package (ADR 0010).
type Recipe struct {
	// ID is the skill's id — the `/<id>` the user invokes it by.
	ID string
	// Plan is the recipe's stages. A stage's prompt file is named under Dir, the address the
	// skill's own folder is announced by.
	Plan Plan
	// Inputs are the recipe's declared inputs, in the order bare tokens bind (BindInputs).
	Inputs []InputDecl
	// Dir is the address of the skill's folder: an absolute host path for a skill found on disk,
	// `shipped:<id>` for one apogee ships embedded.
	Dir string
	// Files is the skill's folder itself, rooted at Dir, so its prompt files and scripts can be
	// read whatever Dir spells. Nil when the folder cannot be opened.
	Files fs.FS
}

// RecipeSource is the port a skill catalog serves recipes through. The skills catalog implements
// it; the agent reads it off its skill resolver, and a test or an embedder may implement its own.
type RecipeSource interface {
	// Recipe returns the recipe the skill id carries, and false when id names no skill or a skill
	// without a recipe. The returned Plan is the caller's own copy.
	Recipe(id string) (Recipe, bool)
	// RecipeIDs lists every skill that carries a recipe, sorted.
	RecipeIDs() []string
}
