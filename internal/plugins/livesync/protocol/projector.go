package protocol

import (
	"fmt"
	"sort"
	"strings"
)

type Projector struct {
	chunks map[string]string
	docs   map[string]Document
}

func NewProjector() *Projector {
	return &Projector{
		chunks: map[string]string{},
		docs:   map[string]Document{},
	}
}

func (p *Projector) Apply(records []Record) error {
	for _, record := range records {
		if record.Chunk != nil {
			p.chunks[record.Chunk.ID] = record.Chunk.Data
			continue
		}
		if record.Document != nil {
			doc := *record.Document
			if doc.ID == "" {
				return fmt.Errorf("document row has empty _id")
			}
			p.docs[doc.ID] = doc
		}
	}
	return nil
}

func (p *Projector) Files() (map[string]File, error) {
	out := map[string]File{}
	docIDs := make([]string, 0, len(p.docs))
	for id := range p.docs {
		docIDs = append(docIDs, id)
	}
	sort.Strings(docIDs)
	for _, id := range docIDs {
		doc := p.docs[id]
		if doc.Path == "" {
			continue
		}
		if doc.IsDeleted() {
			continue
		}
		content, err := p.documentContent(doc)
		if err != nil {
			return nil, err
		}
		out[doc.Path] = File{Path: doc.Path, Content: []byte(content), Mtime: doc.Mtime}
	}
	return out, nil
}

func (p *Projector) DeletedFiles() map[string]File {
	out := map[string]File{}
	for _, doc := range p.docs {
		if doc.Path == "" || !doc.IsDeleted() {
			continue
		}
		out[doc.Path] = File{Path: doc.Path, Deleted: true, Mtime: doc.Mtime}
	}
	return out
}

func (p *Projector) documentContent(doc Document) (string, error) {
	if doc.Type != "" && doc.Type != "plain" {
		return "", fmt.Errorf("unsupported LiveSync document type %q for %s", doc.Type, doc.Path)
	}
	if len(doc.Children) > 0 {
		var b strings.Builder
		for _, child := range doc.Children {
			data, ok := p.chunks[child]
			if !ok {
				if eden, edenOK := doc.Eden[child]; edenOK {
					data = eden.Data
				} else {
					return "", fmt.Errorf("missing chunk %s for %s", child, doc.Path)
				}
			}
			b.WriteString(data)
		}
		return b.String(), nil
	}
	switch data := doc.Data.(type) {
	case string:
		return data, nil
	case []any:
		var b strings.Builder
		for _, part := range data {
			s, ok := part.(string)
			if !ok {
				return "", fmt.Errorf("unsupported data item in %s", doc.Path)
			}
			b.WriteString(s)
		}
		return b.String(), nil
	case nil:
		return "", nil
	default:
		return "", fmt.Errorf("unsupported data value in %s", doc.Path)
	}
}
