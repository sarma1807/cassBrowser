#!/bin/bash

# Dynamic Enhanced Build Script - macOS
# Reads configuration from app.properties and injects at build time
# Usage: ./<script_name> [build|clean|run|rebuild|tidy|help]

# Change to script directory
cd "$(dirname "$0")"
SCRIPT_NAME=$(basename "$0")

# Load configuration from app.properties
if [ -f "app.properties" ]; then
    BINARY_NAME=$(grep "^APP_COMPILE_NAME=" app.properties | cut -d'=' -f2 | tr -d ' ')
    APP_DISPLAY=$(grep "^APP_DISPLAY_NAME=" app.properties | cut -d'=' -f2 | sed 's/^[[:space:]]*//;s/[[:space:]]*$//')
    APP_VER=$(grep "^APP_VERSION=" app.properties | cut -d'=' -f2 | tr -d ' ')
    APP_DATE=$(grep "^APP_VERSION_DATE=" app.properties | cut -d'=' -f2 | tr -d ' ')
    ENCRYPT_CONN=$(grep "^ENCRYPT_CONNECTION_DETAILS=" app.properties | cut -d'=' -f2 | tr -d ' ')
fi

# Fallback if not found
if [ -z "$BINARY_NAME" ]; then
    BINARY_NAME="cassBrowser"
    echo "WARNING : app.properties not found, using defaults"
fi
if [ -z "$APP_DISPLAY" ]; then
    APP_DISPLAY="Cassandra Browser"
fi
if [ -z "$APP_VER" ]; then
    APP_VER="1.0"
fi
if [ -z "$APP_DATE" ]; then
    APP_DATE="20-May-2026"
fi
if [ -z "$ENCRYPT_CONN" ]; then
    ENCRYPT_CONN="yes"
fi

BUILD_DIR="."

# External dependencies required by this project (beyond go.mod defaults)
REQUIRED_DEPS=(
    "github.com/parquet-go/parquet-go"
)

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

function print_header() {
    echo -e "${BLUE}==================================${NC}"
    echo -e "${BLUE}  $1${NC}"
    echo -e "${BLUE}==================================${NC}"
}

function print_error() {
    echo -e "${RED}ERROR : $1${NC}"
}

function print_success() {
    echo -e "${GREEN}$1${NC}"
}

function print_warning() {
    echo -e "${YELLOW}$1${NC}"
}

function check_os_compat() {
    local os
    os=$(uname -s)

    if [ "$os" != "Darwin" ]; then
        print_error "Unsupported operating system : $os"
        echo ""
        case "$os" in
            Linux)
                echo "Detected : Linux"
                echo "This script is for macOS only."
                echo "Use the Linux build script instead."
                ;;
            MINGW*|MSYS*|CYGWIN*)
                echo "Detected : Windows"
                echo "This script is for macOS only."
                echo "Use the Windows build script instead."
                ;;
            *)
                echo "Detected : $os"
                echo "This script is for macOS only."
                ;;
        esac
        echo ""
        exit 1
    fi

    local macos_version
    macos_version=$(sw_vers -productVersion)
    local macos_major
    macos_major=$(echo "$macos_version" | cut -d'.' -f1)

    if [ "$macos_major" -lt 12 ]; then
        print_warning "macOS $macos_version detected — macOS 12 (Monterey) or later is recommended"
        echo ""
    fi
}

function check_gui() {
    # WindowServer is the macOS display compositor — absent means no GUI session
    if ! pgrep -x "WindowServer" &>/dev/null; then
        print_error "No graphical desktop environment detected"
        echo ""
        echo "$APP_DISPLAY is a desktop GUI application and cannot run on a"
        echo "headless (CLI-only) system."
        echo ""
        echo "macOS requires WindowServer to be running for GUI applications."
        echo "Ensure you are running this script from a macOS desktop session,"
        echo "not a headless SSH connection."
        echo ""
        exit 1
    fi
}

function check_go() {
    if ! command -v go &> /dev/null; then
        print_error "Go is not installed or not in PATH"
        echo ""
        local fix_script="/tmp/${BINARY_NAME}_fixes.sh"
        {
            echo "#!/bin/bash"
            echo "# Fixes for ${BINARY_NAME} - generated $(date '+%Y-%m-%d %H:%M:%S')"
            echo ""
            echo "brew install go"
        } > "$fix_script"
        chmod +x "$fix_script"
        echo "A fix script has been created. Run it as your user :"
        echo "  bash $fix_script"
        echo ""
        echo "Note : Alternatively, install the latest Go from the official installer :"
        echo "       https://go.dev/doc/install"
        echo ""
        exit 1
    fi
}

function check_build_reqs() {
    local need_go=0
    local missing_items=()
    local has_issues=0

    if ! command -v go &> /dev/null; then
        need_go=1
        has_issues=1
        print_error "Go is not installed or not in PATH"
        echo ""
    fi

    # Xcode Command Line Tools provide clang (CGO compiler) on macOS
    if ! xcode-select -p &>/dev/null; then
        missing_items+=("xcode-clt")
        has_issues=1
    fi

    # pkg-config is used for dependency resolution during CGO builds
    if ! command -v pkg-config &>/dev/null; then
        missing_items+=("pkg-config")
        has_issues=1
    fi

    if [ ${#missing_items[@]} -gt 0 ]; then
        print_warning "Missing build dependencies detected :"
        for item in "${missing_items[@]}"; do
            echo "  - $item"
        done
        echo ""
    fi

    if [ $has_issues -eq 1 ]; then
        local fix_script="/tmp/${BINARY_NAME}_fixes.sh"
        local needs_xcode_clt=0
        local brew_pkgs=()

        for item in "${missing_items[@]}"; do
            case "$item" in
                xcode-clt)
                    needs_xcode_clt=1
                    ;;
                pkg-config)
                    brew_pkgs+=("pkg-config")
                    ;;
            esac
        done
        [ $need_go -eq 1 ] && brew_pkgs+=("go")

        {
            echo "#!/bin/bash"
            echo "# Fixes for ${BINARY_NAME} - generated $(date '+%Y-%m-%d %H:%M:%S')"
            echo ""
            if [ $needs_xcode_clt -eq 1 ]; then
                echo "# Install Xcode Command Line Tools (provides clang/gcc for CGO)"
                echo "xcode-select --install"
                echo ""
            fi
            if [ ${#brew_pkgs[@]} -gt 0 ]; then
                echo "# Install Homebrew packages"
                echo "brew install ${brew_pkgs[*]}"
            fi
        } > "$fix_script"
        chmod +x "$fix_script"
        echo "A fix script has been created. Run it as your user :"
        echo "  bash $fix_script"
        echo ""
        return 1
    fi

    return 0
}

function fetch_deps() {
    local failed=0
    for dep in "${REQUIRED_DEPS[@]}"; do
        if ! grep -q "$dep" go.mod 2>/dev/null; then
            echo "Fetching dependency : $dep"
            if ! go get "$dep" 2>&1; then
                print_error "Failed to fetch $dep"
                failed=1
            fi
        fi
    done
    return $failed
}

function tidy() {
    print_header "Running go mod tidy"
    echo ""

    check_go

    echo "Go version : $(go version)"
    echo ""

    # Check if go.mod exists
    if [ ! -f "go.mod" ]; then
        print_error "go.mod not found. Run 'go mod init' first."
        return 1
    fi

    echo "Fetching required dependencies ..."
    if ! fetch_deps; then
        print_error "Failed to fetch required dependencies"
        return 1
    fi

    echo "Tidying dependencies ..."
    if go mod tidy 2>&1; then
        echo ""
        print_success "go mod tidy completed successfully"
        echo ""

        # Show go.mod summary
        echo "Dependencies in go.mod:"
        grep -E "^\t" go.mod 2>/dev/null | head -10

        dep_count=$(grep -cE "^\t" go.mod 2>/dev/null; true)
        if [ "$dep_count" -gt 10 ]; then
            echo "  ... and $((dep_count - 10)) more"
        fi

        return 0
    else
        echo ""
        print_error "go mod tidy failed"
        return 1
    fi
}

function build() {
    print_header "Building $BINARY_NAME"
    echo ""
    echo -e "${BLUE}Configuration :${NC}"
    echo "  App Binary Name     : $BINARY_NAME"
    echo "  App Display Name    : $APP_DISPLAY"
    echo "  App Version         : $APP_VER"
    echo "  App Build Date      : $APP_DATE"
    echo "  Encrypt Connections : $ENCRYPT_CONN"
    echo ""

    echo "Checking system requirements ..."
    if ! check_build_reqs; then
        print_error "Install missing requirements and retry"
        return 1
    fi
    print_success "System requirements verified"
    echo ""

    echo "Go version : $(go version)"
    echo ""

    # Fetch any missing required dependencies
    echo "Checking required dependencies ..."
    if ! fetch_deps; then
        print_error "Failed to fetch required dependencies"
        return 1
    fi

    # Run go mod tidy to ensure dependencies are consistent
    echo "Ensuring dependencies are up to date ..."
    if ! go mod tidy 2>&1; then
        print_error "go mod tidy failed"
        return 1
    fi
    print_success "Dependencies verified"
    echo ""

    # Format code
    echo "Formatting code ..."
    if ! go fmt ./... 2>&1; then
        print_error "go fmt failed"
        return 1
    fi
    print_success "Code formatted"
    echo ""

    # Check for issues
    echo "Checking code (might take several minutes) ..."
    if ! go vet ./... 2>&1; then
        print_error "go vet found issues"
        return 1
    fi
    print_success "No issues found"
    echo ""

    # Build the application with injected variables
    echo "Compiling ..."
    if go build -v -ldflags "-X main.AppName=$BINARY_NAME -X 'main.AppDisplayName=$APP_DISPLAY' -X main.AppVersion=$APP_VER -X main.AppVersionDate=$APP_DATE -X main.EncryptConnectionDetails=$ENCRYPT_CONN" -o "$BINARY_NAME" "$BUILD_DIR"; then
        echo ""
        print_header "Build Successful!"
        echo ""
        ls -lh "$BINARY_NAME"
        echo ""

        # Write build info file
        BUILD_INFO_FILE="${BINARY_NAME}_BuildInfo_Mac.txt"
        VER_GOCQL=$(grep 'github.com/gocql/gocql' go.mod 2>/dev/null | awk '{print $2}' | head -1)
        VER_DUCKDB=$(grep 'github.com/duckdb/duckdb-go/v2' go.mod 2>/dev/null | awk '{print $2}' | head -1)
        VER_PARQUET=$(grep 'github.com/parquet-go/parquet-go' go.mod 2>/dev/null | awk '{print $2}' | head -1)
        {
            echo ""
            echo "Build Date       : $(date '+%Y-%m-%d %H:%M:%S')"
            echo "App Binary  Name : $BINARY_NAME"
            echo "App Display Name : $APP_DISPLAY"
            echo "App Version      : $APP_VER"
            echo "App Build Date   : $APP_DATE"
            BINARY_BYTES=$(stat -f '%z' "$BINARY_NAME")
            BINARY_HUMAN=$(du -sh "$BINARY_NAME" | awk '{print $1}')
            echo "Binary Size      : $BINARY_BYTES bytes ($BINARY_HUMAN)"
            echo ""
            echo "OS               : $(uname -s)"
            echo "OS Version       : $(sw_vers -productVersion)"
            echo "Architecture     : $(uname -m)"
            echo "Distribution     : $(sw_vers -productName) $(sw_vers -productVersion)"
            echo ""
            echo "Go Version       : $(go version)"
            echo ""
            echo "gocql            : ${VER_GOCQL:-not found}"
            echo "go-duckdb        : ${VER_DUCKDB:-not found}"
            echo "parquet-go       : ${VER_PARQUET:-not found}"
            echo ""
            echo "MD5              : $(md5 -q "$BINARY_NAME")"
            echo "SHA256           : $(shasum -a 256 "$BINARY_NAME" | awk '{print $1}')"
            echo "SHA512           : $(shasum -a 512 "$BINARY_NAME" | awk '{print $1}')"
            echo ""
        } > "$BUILD_INFO_FILE"
        print_success "Build info written to : $BUILD_INFO_FILE"
        echo ""

        print_success "Run with : ./$BINARY_NAME"
        return 0
    else
        echo ""
        print_header "Build Failed!"
        print_error "Compilation errors occurred"
        return 1
    fi
}

function clean() {
    print_header "Cleaning"
    echo ""

    local cleaned=0

    if [ -f "$BINARY_NAME" ]; then
        echo "Removing binary : $BINARY_NAME"
        if rm -f "$BINARY_NAME"; then
            cleaned=1
        else
            print_error "Failed to remove $BINARY_NAME"
            return 1
        fi
    fi

    if [ -f "$BINARY_NAME.old" ]; then
        echo "Removing old binary : $BINARY_NAME.old"
        if rm -f "$BINARY_NAME.old"; then
            cleaned=1
        else
            print_error "Failed to remove $BINARY_NAME.old"
            return 1
        fi
    fi

    if [ -f "${BINARY_NAME}_BuildInfo_Mac.txt" ]; then
        echo "Removing build info file : ${BINARY_NAME}_BuildInfo_Mac.txt"
        if rm -f "${BINARY_NAME}_BuildInfo_Mac.txt"; then
            cleaned=1
        else
            print_error "Failed to remove ${BINARY_NAME}_BuildInfo_Mac.txt"
            return 1
        fi
    fi

    if [ -d "$BUILD_DIR/.cgolibs" ]; then
        echo "Removing CGO libs directory : $BUILD_DIR/.cgolibs"
        if rm -rf "$BUILD_DIR/.cgolibs"; then
            cleaned=1
        else
            print_error "Failed to remove $BUILD_DIR/.cgolibs"
            return 1
        fi
    fi

    if [ $cleaned -eq 0 ]; then
        echo "Nothing to clean"
    fi

    echo ""
    print_success "Clean complete"
}

function run() {
    if [ ! -f "$BINARY_NAME" ]; then
        print_warning "Binary not found. Building first ..."
        echo ""
        if ! build; then
            print_error "Build failed, cannot run"
            exit 1
        fi
        echo ""
    fi

    print_header "Running $BINARY_NAME"
    echo ""
    "./$BINARY_NAME"
}

function rebuild() {
    if ! clean; then
        print_error "Clean failed"
        exit 1
    fi
    echo ""
    if ! build; then
        print_error "Build failed"
        exit 1
    fi
}

function fmt() {
    print_header "Formatting Code"
    echo ""

    check_go

    echo "Running go fmt ..."
    if go fmt ./...; then
        echo ""
        print_success "Code formatting complete"
        return 0
    else
        print_error "go fmt failed"
        return 1
    fi
}

function vet() {
    print_header "Checking Code"
    echo ""

    check_go

    echo "Running go vet ..."
    if go vet ./... 2>&1; then
        echo ""
        print_success "No issues found"
        return 0
    else
        echo ""
        print_warning "Issues found (see above)"
        return 1
    fi
}

function update() {
    print_header "Updating Dependencies"
    echo ""

    check_go

    echo "Go version : $(go version)"
    echo ""

    if [ ! -f "go.mod" ]; then
        print_error "go.mod not found. Run 'go mod init' first."
        return 1
    fi

    BACKUP_TS=$(date '+%Y%m%d_%H%M%S')

    GOMOD_BACKUP="go.mod_${BACKUP_TS}"
    echo "Backing up go.mod to : $GOMOD_BACKUP"
    if ! cp go.mod "$GOMOD_BACKUP"; then
        print_error "Failed to backup go.mod"
        return 1
    fi
    print_success "Backup created : $GOMOD_BACKUP"

    if [ -f "go.sum" ]; then
        GOSUM_BACKUP="go.sum_${BACKUP_TS}"
        echo "Backing up go.sum to : $GOSUM_BACKUP"
        if ! cp go.sum "$GOSUM_BACKUP"; then
            print_error "Failed to backup go.sum"
            return 1
        fi
        print_success "Backup created : $GOSUM_BACKUP"
    fi
    echo ""

    echo "Upgrading all dependencies to latest versions ..."
    if ! go get -u ./... 2>&1; then
        print_error "go get -u failed"
        return 1
    fi
    print_success "Dependencies upgraded"
    echo ""

    echo "Tidying dependencies ..."
    if go mod tidy 2>&1; then
        echo ""
        print_success "go mod tidy completed successfully"
        echo ""

        echo "Dependencies in go.mod:"
        grep -E "^\t" go.mod 2>/dev/null | head -10

        dep_count=$(grep -cE "^\t" go.mod 2>/dev/null; true)
        if [ "$dep_count" -gt 10 ]; then
            echo "  ... and $((dep_count - 10)) more"
        fi

        echo ""
        print_success "Run './$SCRIPT_NAME build' to rebuild with updated dependencies"
        return 0
    else
        echo ""
        print_error "go mod tidy failed"
        return 1
    fi
}

function show_help() {
    echo "$BINARY_NAME Build Tool"
    echo ""
    echo "Usage: ./$SCRIPT_NAME [command]"
    echo ""
    echo "Commands:"
    echo "  build     - Compile the application (default)"
    echo "  clean     - Remove compiled binaries"
    echo "  run       - Build (if needed) and run the application"
    echo "  rebuild   - Clean and build"
    echo "  tidy      - Run go mod tidy to update dependencies"
    echo "  update    - Upgrade all dependencies to latest versions"
    echo "  fmt       - Format code with go fmt"
    echo "  vet       - Check code with go vet"
    echo "  help      - Show this help message"
    echo ""
    echo "Examples:"
    echo "  ./$SCRIPT_NAME           # Rebuild (clean + build, default)"
    echo "  ./$SCRIPT_NAME build     # Build (includes go mod tidy)"
    echo "  ./$SCRIPT_NAME clean     # Clean"
    echo "  ./$SCRIPT_NAME run       # Run (builds if needed)"
    echo "  ./$SCRIPT_NAME rebuild   # Clean and build"
    echo "  ./$SCRIPT_NAME tidy      # Update dependencies only"
    echo "  ./$SCRIPT_NAME update    # Upgrade all dependencies to latest versions"
    echo "  ./$SCRIPT_NAME fmt       # Format code"
    echo "  ./$SCRIPT_NAME vet       # Check for issues"
    echo ""
    echo "Note : App name is read from app.properties (APP_COMPILE_NAME=$BINARY_NAME)"
}

# Step 1 : verify OS compatibility
check_os_compat

# Step 2 : verify graphical environment
check_gui

# Parse command
COMMAND="${1:-rebuild}"

case "$COMMAND" in
    build)
        build
        ;;
    clean)
        clean
        ;;
    run)
        run
        ;;
    rebuild)
        rebuild
        ;;
    tidy)
        tidy
        ;;
    update)
        update
        ;;
    fmt)
        fmt
        ;;
    vet)
        vet
        ;;
    help|--help|-h)
        show_help
        ;;
    *)
        print_error "Unknown command : $COMMAND"
        echo ""
        show_help
        exit 1
        ;;
esac
