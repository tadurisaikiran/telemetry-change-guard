package domain

// SourceLocation identifies where a consumer or reference was discovered.
// File is populated for local-first adapters; URL and Repo are reserved for
// explicit remote sources.
type SourceLocation struct {
	File   string `json:"file,omitempty"`
	Line   int    `json:"line,omitempty"`
	Column int    `json:"column,omitempty"`
	URL    string `json:"url,omitempty"`
	Repo   string `json:"repo,omitempty"`
}

// Owner identifies a consumer's responsible team or individual when known.
type Owner struct {
	Name  string `json:"name"`
	Email string `json:"email,omitempty"`
}
