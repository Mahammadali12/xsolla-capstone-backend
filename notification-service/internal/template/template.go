// Package template renders notification messages from template files.
package template

import (
	"bytes"
	"encoding/json"
	"fmt"
	htmltemplate "html/template"
	"os"
	"path/filepath"
	"strings"
	texttemplate "text/template"

	"xsolla-notification-service/internal/channel"
	"xsolla-notification-service/internal/events"
)

const (
	subjectSuffix = ".subject.tmpl"
	bodySuffix    = ".body.tmpl"
	htmlSuffix    = ".html.tmpl"
	// layoutFile wraps every HTML body; it is not a policy template itself.
	layoutFile = "_layout.html.tmpl"
)

// data is the root value templates execute against. Payload is the event's
// data object; Event and Recipient carry the metadata templates may reference.
type data struct {
	Event     events.Envelope
	Recipient events.Recipient
	Payload   map[string]any
}

// layoutData is the root value for the shared HTML layout. Content is already
// escaped by the fragment render, so it must not be escaped again.
type layoutData struct {
	Subject string
	Logo    bool
	Content htmltemplate.HTML
}

type pair struct {
	subject *texttemplate.Template
	body    *texttemplate.Template
	html    *htmltemplate.Template
}

// Set holds the templates loaded at startup, keyed by policy template name.
type Set struct {
	byName map[string]pair
	layout *htmltemplate.Template // nil when no HTML templates are loaded
	logo   bool
}

// NewSet returns an empty set. Rendering against it fails with "not found"; it
// is used when no TEMPLATES_PATH is configured.
func NewSet() *Set {
	return &Set{byName: make(map[string]pair)}
}

// Len reports how many templates are loaded.
func (s *Set) Len() int {
	return len(s.byName)
}

// UseLogo makes HTML bodies render an image instead of the text wordmark. The
// caller enables it only when the email channel attaches the logo.
func (s *Set) UseLogo() {
	s.logo = true
}

// LoadDir loads every <name>.subject.tmpl, <name>.body.tmpl, and optional
// <name>.html.tmpl from dir. HTML bodies are wrapped by _layout.html.tmpl.
func LoadDir(dir string) (*Set, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	type paths struct{ subject, body, html string }
	found := make(map[string]*paths)
	layoutPath := ""
	record := func(fileName, suffix string) {
		name := strings.TrimSuffix(fileName, suffix)
		if found[name] == nil {
			found[name] = &paths{}
		}
		path := filepath.Join(dir, fileName)
		switch suffix {
		case subjectSuffix:
			found[name].subject = path
		case bodySuffix:
			found[name].body = path
		case htmlSuffix:
			found[name].html = path
		}
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		fileName := entry.Name()
		if fileName == layoutFile {
			layoutPath = filepath.Join(dir, fileName)
			continue
		}
		switch {
		case strings.HasSuffix(fileName, subjectSuffix):
			record(fileName, subjectSuffix)
		case strings.HasSuffix(fileName, bodySuffix):
			record(fileName, bodySuffix)
		case strings.HasSuffix(fileName, htmlSuffix):
			record(fileName, htmlSuffix)
		}
	}

	set := NewSet()
	if layoutPath != "" {
		layout, err := parseHTML(layoutPath)
		if err != nil {
			return nil, err
		}
		set.layout = layout
	}

	htmlTemplates := 0
	for name, p := range found {
		if name == "" {
			return nil, fmt.Errorf("%s: template name must not be empty", dir)
		}
		if p.subject == "" {
			return nil, fmt.Errorf("template %q: missing %s", name, subjectSuffix)
		}
		if p.body == "" {
			return nil, fmt.Errorf("template %q: missing %s", name, bodySuffix)
		}
		subject, err := parse(p.subject)
		if err != nil {
			return nil, err
		}
		body, err := parse(p.body)
		if err != nil {
			return nil, err
		}
		var html *htmltemplate.Template
		if p.html != "" {
			htmlTemplates++
			html, err = parseHTML(p.html)
			if err != nil {
				return nil, err
			}
		}
		set.byName[name] = pair{subject: subject, body: body, html: html}
	}
	if htmlTemplates > 0 && set.layout == nil {
		return nil, fmt.Errorf("%s: %s requires %s", dir, htmlSuffix, layoutFile)
	}
	return set, nil
}

func parse(path string) (*texttemplate.Template, error) {
	source, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	parsed, err := texttemplate.New(filepath.Base(path)).Option("missingkey=error").Parse(string(source))
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return parsed, nil
}

// parseHTML uses html/template so payload values are escaped for the context
// they appear in; event data is never trusted markup.
func parseHTML(path string) (*htmltemplate.Template, error) {
	source, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	parsed, err := htmltemplate.New(filepath.Base(path)).Option("missingkey=error").Parse(string(source))
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return parsed, nil
}

// Render renders the named template for one recipient. A missing payload key is
// an error rather than an empty string.
func (s *Set) Render(name string, event events.Envelope, recipient events.Recipient) (channel.Message, error) {
	p, ok := s.byName[name]
	if !ok {
		return channel.Message{}, fmt.Errorf("template %q not found", name)
	}

	var payload map[string]any
	if err := json.Unmarshal(event.Data, &payload); err != nil {
		return channel.Message{}, fmt.Errorf("template %q: decode event data: %w", name, err)
	}
	root := data{Event: event, Recipient: recipient, Payload: payload}

	var subject, body bytes.Buffer
	if err := p.subject.Execute(&subject, root); err != nil {
		return channel.Message{}, fmt.Errorf("template %q subject: %w", name, err)
	}
	if err := p.body.Execute(&body, root); err != nil {
		return channel.Message{}, fmt.Errorf("template %q body: %w", name, err)
	}

	message := channel.Message{
		EventID: event.ID,
		Subject: strings.TrimSpace(subject.String()),
		Body:    body.String(),
	}
	if p.html != nil {
		var fragment bytes.Buffer
		if err := p.html.Execute(&fragment, root); err != nil {
			return channel.Message{}, fmt.Errorf("template %q html: %w", name, err)
		}
		var page bytes.Buffer
		err := s.layout.Execute(&page, layoutData{
			Subject: message.Subject,
			Logo:    s.logo,
			Content: htmltemplate.HTML(fragment.String()),
		})
		if err != nil {
			return channel.Message{}, fmt.Errorf("template %q layout: %w", name, err)
		}
		message.HTMLBody = page.String()
	}
	return message, nil
}
