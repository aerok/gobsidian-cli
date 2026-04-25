package livesync

type EdenChunk struct {
	Data  string `json:"data"`
	Epoch int64  `json:"epoch"`
}

type Document struct {
	ID        string               `json:"_id"`
	Rev       string               `json:"_rev,omitempty"`
	Path      string               `json:"path,omitempty"`
	Ctime     int64                `json:"ctime,omitempty"`
	Mtime     int64                `json:"mtime,omitempty"`
	Size      int64                `json:"size,omitempty"`
	Type      string               `json:"type,omitempty"`
	Children  []string             `json:"children,omitempty"`
	Data      any                  `json:"data,omitempty"`
	Eden      map[string]EdenChunk `json:"eden"`
	Deleted   bool                 `json:"deleted,omitempty"`
	DeletedP  bool                 `json:"_deleted,omitempty"`
	Revisions *RevisionTree        `json:"_revisions,omitempty"`
}

func (d Document) IsDeleted() bool {
	return d.Deleted || d.DeletedP
}

type RevisionTree struct {
	Start int      `json:"start"`
	IDs   []string `json:"ids"`
}

type Chunk struct {
	ID        string
	Data      string
	Encrypted bool
}

type Record struct {
	Document *Document
	Chunk    *Chunk
}

type File struct {
	Path    string
	Content []byte
	Mtime   int64
	Deleted bool
}
