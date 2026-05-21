package main

// Application configuration
// These variables are injected at build time via -ldflags
// Example: go build -ldflags "-X main.AppName=cassBrowser -X 'main.AppDisplayName=Cassandra Browser'"
//
// Values are read from app.properties during build by build.sh and make.sh scripts
// At runtime, app.properties is NOT required - all values are hardcoded in the binary

// AppTagline is the default status bar message shown when no specific context is active.
const AppTagline = "Smart GUI for browsing data in Apache Cassandra clusters"

var (
	// AppName is the binary name and config directory name (from APP_COMPILE_NAME)
	AppName = "cassBrowser"

	// AppDisplayName is shown in the UI title and about popup (from APP_DISPLAY_NAME)
	AppDisplayName = "Cassandra Browser"

	// AppVersion is the version number shown in about popup (from APP_VERSION)
	AppVersion = "1.0"

	// AppVersionDate is the version date shown in about popup (from APP_VERSION_DATE)
	AppVersionDate = "20-May-2026"

	// EncryptConnectionDetails determines if connection details are encrypted (from ENCRYPT_CONNECTION_DETAILS)
	// Values: "yes" = encrypt, "no" = plain text
	EncryptConnectionDetails = "yes"
)
