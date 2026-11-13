// Package template renders notification messages from template files.
package template

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/template"

	"xsolla-notification-service/internal/channel"
	"xsolla-notification-service/internal/events"
)

const (
	subjectSuffix = ".subject.tmpl"
	bodySuffix    = ".body.tmpl"
)

// data is the root value templates execute against. Payload is the event's
// data object; Event and Recipient carry the metadata templates may reference.
type data struct {
	Event     events.Envelope
	Recipient events.Recipient
	Payload   map[string]any
}

type pair struct {
	subject *template.Template
	body    *template.Template
}

// Set holds the templates loaded at startup, keyed by policy template name.
type Set struct {
	byName map[string]pair
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

// LoadDir loads every <name>.subject.tmpl and <name>.body.tmpl pair from dir.
func LoadDir(dir string) (*Set, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	type paths struct{ subject, body string }
	found := make(map[string]*paths)
	record := func(fileName, suffix string) {
		name := strings.TrimSuffix(fileName, suffix)
		if found[name] == nil {
			found[name] = &paths{}
		}
		path := filepath.Join(dir, fileName)
		if suffix == subjectSuffix {
			found[name].subject = path
		} else {
			found[name].body = path
		}
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		fileName := entry.Name()
		switch {
		case strings.HasSuffix(fileName, subjectSuffix):
			record(fileName, subjectSuffix)
		case strings.HasSuffix(fileName, bodySuffix):
			record(fileName, bodySuffix)
		}
	}

	set := NewSet()
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
		set.byName[name] = pair{subject: subject, body: body}
	}
	return set, nil
}

func parse(path string) (*template.Template, error) {
	source, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	parsed, err := template.New(filepath.Base(path)).Option("missingkey=error").Parse(string(source))
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
	return channel.Message{EventID: event.ID, Subject: strings.TrimSpace(subject.String()), Body: body.String()}, nil
}
