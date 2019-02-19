package updater

// Info is a metadata about the binary file
type Info struct {
	Name        string `json:"name"`
	Version     string `json:"version"`
	Description string `json:"description"`
}
