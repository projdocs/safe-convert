package internal

import "strings"

// knownMIMETypes maps each recognised MIME type to its canonical file
// extension (with leading dot). This is the single source of truth for both
// validation and extension lookup — no other package should maintain its own
// MIME→extension mapping.
//
// Adding support for a new format requires only an entry here.
var knownMIMETypes = map[string]string{
	// Word processing
	"application/vnd.openxmlformats-officedocument.wordprocessingml.document": ".docx",
	"application/vnd.openxmlformats-officedocument.wordprocessingml.template": ".dotx",
	"application/msword":                               ".doc",
	"application/vnd.oasis.opendocument.text":          ".odt",
	"application/vnd.oasis.opendocument.text-template": ".ott",
	"text/plain":      ".txt",
	"text/html":       ".html",
	"application/rtf": ".rtf",
	"text/rtf":        ".rtf",

	// Spreadsheets
	"application/vnd.openxmlformats-officedocument.spreadsheetml.sheet":    ".xlsx",
	"application/vnd.openxmlformats-officedocument.spreadsheetml.template": ".xltx",
	"application/vnd.ms-excel":                                ".xls",
	"application/vnd.oasis.opendocument.spreadsheet":          ".ods",
	"application/vnd.oasis.opendocument.spreadsheet-template": ".ots",
	"text/csv": ".csv",

	// Presentations
	"application/vnd.openxmlformats-officedocument.presentationml.presentation": ".pptx",
	"application/vnd.openxmlformats-officedocument.presentationml.template":     ".potx",
	"application/vnd.ms-powerpoint":                                             ".ppt",
	"application/vnd.oasis.opendocument.presentation":                           ".odp",
	"application/vnd.oasis.opendocument.presentation-template":                  ".otp",

	// Drawings
	"application/vnd.oasis.opendocument.graphics": ".odg",
	"image/svg+xml": ".svg",

	// Visio
	"application/vnd.visio":                            ".vsd",
	"application/vnd.ms-visio.drawing":                 ".vsdx",
	"application/vnd.ms-visio.drawing.macroenabled.12": ".vsdm",
}

// IsKnownMIMEType reports whether the given media type (without parameters)
// is a recognised document type that LibreOffice can convert to PDF, and
// returns its canonical file extension (e.g. ".docx").
//
// This is a membership check only — it does not enforce an allowlist.
// Returns ("", false) if the media type is not recognised.
func IsKnownMIMEType(mediaType string) (ext string, ok bool) {
	mediaType = strings.ToLower(strings.TrimSpace(mediaType))
	ext, ok = knownMIMETypes[mediaType]
	return
}
