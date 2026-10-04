package domain

import "time"

// File metadata is public to its owner; storage locators and content are private.
type File struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Kind      string    `json:"kind"`
	MIME      string    `json:"mime"`
	Size      int64     `json:"size"`
	OriginURL string    `json:"origin_url,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

type Table struct {
	Name string     `json:"name"`
	Rows [][]string `json:"rows"`
}

type FileContent struct {
	Text   string  `json:"text"`
	Tables []Table `json:"tables"`
}
