// Package settings feeds the settings popup: the open request's settings,
// written in its `# steep` block over the config's defaults.
package settings

import (
	"github.com/anotherhadi/foam/form"

	"github.com/anotherhadi/steep/internal/request"
)

// Request edits the settings of the request whose text Text and SetText
// access.
type Request struct {
	Text     func() string
	SetText  func(text string)
	Defaults map[string]string
}

func (r Request) Fields() []form.Field {
	f := request.Parse(r.Text())
	eff := f.Effective(r.Defaults)
	out := make([]form.Field, 0, len(request.Settings))
	for _, s := range request.Settings {
		origin := "config"
		if _, ok := f.Get(s.Key); ok {
			origin = "request"
		}
		out = append(out, form.Field{ID: s.Key, Label: s.Label, Help: s.Help, Value: eff[s.Key], Choices: s.Choices, Origin: origin})
	}
	return out
}

func (r Request) Set(id, value string) error {
	value = request.Normalize(id, value)
	if err := request.Validate(id, value); err != nil {
		return err
	}
	f := request.Parse(r.Text())
	if value == r.Defaults[id] {
		f.Unset(id)
	} else {
		f.Set(id, value)
	}
	r.SetText(f.String())
	return nil
}

func (r Request) Reset(id string) error {
	f := request.Parse(r.Text())
	f.Unset(id)
	r.SetText(f.String())
	return nil
}
