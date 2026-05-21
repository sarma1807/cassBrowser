#!/bin/bash

# Dynamic Enhanced Build Script
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

    if [ "$os" != "Linux" ]; then
        print_error "Unsupported operating system : $os"
        echo ""
        case "$os" in
            Darwin)
                echo "Detected : macOS"
                echo "This script is for RHEL-compatible Linux only."
                echo "Use the macOS build script instead."
                ;;
            MINGW*|MSYS*|CYGWIN*)
                echo "Detected : Windows"
                echo "This script is for RHEL-compatible Linux only."
                echo "Use the Windows build script instead."
                ;;
            *)
                echo "Detected : $os"
                echo "This script is for RHEL-compatible Linux only."
                ;;
        esac
        echo ""
        exit 1
    fi

    if [ ! -f /etc/os-release ]; then
        print_error "Cannot determine Linux distribution (/etc/os-release not found)"
        echo ""
        echo "This script requires a RHEL-compatible Linux distribution."
        echo ""
        exit 1
    fi

    local distro_id
    distro_id=$(get_distro_id)
    local id_like
    id_like=$(grep '^ID_LIKE=' /etc/os-release | cut -d'=' -f2 | tr -d '"' | tr '[:upper:]' '[:lower:]')
    local pretty_name
    pretty_name=$(grep '^PRETTY_NAME=' /etc/os-release | cut -d'=' -f2 | tr -d '"')

    local is_rhel_family=0
    case "$distro_id" in
        rhel|rocky|almalinux|centos|fedora|ol)
            is_rhel_family=1
            ;;
    esac

    if [ $is_rhel_family -eq 0 ] && echo "$id_like" | grep -qE '(rhel|fedora|centos)'; then
        is_rhel_family=1
    fi

    if [ $is_rhel_family -eq 0 ]; then
        print_error "Unsupported Linux distribution : ${pretty_name:-$distro_id}"
        echo ""
        echo "This script is designed for RHEL-compatible distributions :"
        echo "  - Red Hat Enterprise Linux (RHEL)"
        echo "  - Rocky Linux"
        echo "  - AlmaLinux"
        echo "  - CentOS Stream"
        echo "  - Oracle Linux"
        echo ""
        echo "Detected : ${pretty_name:-$distro_id}"
        echo ""
        echo "The package manager commands (dnf, rpm) and repository"
        echo "management in this script will not work on your distribution."
        echo ""
        exit 1
    fi

    if ! command -v rpm &>/dev/null; then
        print_error "rpm command not found"
        echo ""
        echo "This script requires an RPM-based system."
        echo ""
        exit 1
    fi
}

function check_gui() {
    # X11 session active
    [ -n "$DISPLAY" ] && return 0

    # Wayland session active
    [ -n "$WAYLAND_DISPLAY" ] && return 0

    # System configured for graphical target (DISPLAY may not be exported to this shell)
    local default_target
    default_target=$(systemctl get-default 2>/dev/null)
    [ "$default_target" = "graphical.target" ] && return 0

    # Running X server process (fallback for non-systemd or unusual setups)
    pgrep -x "Xorg" &>/dev/null && return 0
    pgrep -x "X" &>/dev/null && return 0

    print_error "No graphical desktop environment detected"
    echo ""
    echo "$APP_DISPLAY is a desktop GUI application and cannot run on a"
    echo "headless (CLI-only) system."
    echo ""
    echo "This system appears to have no graphical environment. You need :"
    echo "  - A graphical desktop environment (GNOME, KDE, XFCE, etc.)"
    echo "  - Or an X11 display server with the DISPLAY variable set"
    echo ""
    exit 1
}

function get_distro_id() {
    if [ -f /etc/os-release ]; then
        grep '^ID=' /etc/os-release | cut -d'=' -f2 | tr -d '"' | tr '[:upper:]' '[:lower:]'
    else
        echo "unknown"
    fi
}

function check_go() {
    if ! command -v go &> /dev/null; then
        print_error "Go is not installed or not in PATH"
        echo ""
        local fix_script="/tmp/${BINARY_NAME}_root_fixes.sh"
        {
            echo "#!/bin/bash"
            echo "# Root fixes for ${BINARY_NAME} - generated $(date '+%Y-%m-%d %H:%M:%S')"
            echo ""
            echo "dnf install -y golang"
        } > "$fix_script"
        chmod +x "$fix_script"
        echo "A root fix script has been created. Run it as 'root' user :"
        echo "  bash $fix_script"
        echo ""
        exit 1
    fi
}

function check_build_reqs() {
    local need_go=0
    local missing_pkgs=()
    local has_issues=0

    if ! command -v go &> /dev/null; then
        need_go=1
        has_issues=1
        print_error "Go is not installed or not in PATH"
        echo ""
    fi

    local required_pkgs=(
        "gcc"
        "pkg-config"
        "libstdc++-devel"
        "mesa-libGL-devel"
        "mesa-libGLU-devel"
        "libX11-devel"
        "libXrandr-devel"
        "libXcursor-devel"
        "libXinerama-devel"
        "libXi-devel"
        "libXxf86vm-devel"
    )

    for pkg in "${required_pkgs[@]}"; do
        if ! rpm -q --whatprovides "$pkg" &>/dev/null; then
            missing_pkgs+=("$pkg")
            has_issues=1
        fi
    done

    if [ ${#missing_pkgs[@]} -gt 0 ]; then
        print_warning "Missing build dependencies detected :"
        for pkg in "${missing_pkgs[@]}"; do
            echo "  - $pkg"
        done
        echo ""
    fi

    if [ $has_issues -eq 1 ]; then
        local fix_script="/tmp/${BINARY_NAME}_root_fixes.sh"

        # Packages that live in the CRB repository on RHEL 9-family distros
        local crb_pkgs=("mesa-libGLU-devel" "libXxf86vm-devel")

        # Check if any missing package requires CRB
        local needs_crb=0
        for pkg in "${missing_pkgs[@]}"; do
            for crb_pkg in "${crb_pkgs[@]}"; do
                if [ "$pkg" = "$crb_pkg" ]; then
                    needs_crb=1
                    break 2
                fi
            done
        done

        # Resolve distro-specific CRB enable command
        local distro_id=""
        local rhel_major=""
        local crb_cmd=""
        local pre_crb_cmd=""
        if [ $needs_crb -eq 1 ]; then
            distro_id=$(get_distro_id)
            case "$distro_id" in
                rhel)
                    rhel_major=$(. /etc/os-release && echo "${VERSION_ID%%.*}")
                    crb_cmd="subscription-manager repos --enable codeready-builder-for-rhel-${rhel_major}-$(uname -m)-rpms"
                    ;;
                rocky|almalinux|centos)
                    pre_crb_cmd="dnf install -y dnf-plugins-core"
                    crb_cmd="dnf config-manager --set-enabled crb"
                    ;;
                *)
                    crb_cmd=""
                    ;;
            esac
        fi

        {
            echo "#!/bin/bash"
            echo "# Root fixes for ${BINARY_NAME} - generated $(date '+%Y-%m-%d %H:%M:%S')"
            echo ""
            if [ $needs_crb -eq 1 ]; then
                echo "# Enable CodeReady Builder (CRB) repository"
                if [ -n "$crb_cmd" ]; then
                    [ -n "$pre_crb_cmd" ] && echo "$pre_crb_cmd"
                    echo "$crb_cmd"
                else
                    echo "# WARNING : Unrecognised distribution '${distro_id}' - manually enable CRB first :"
                    echo "# Rocky / AlmaLinux / CentOS Stream : dnf install -y dnf-plugins-core && dnf config-manager --set-enabled crb"
                    echo "# RHEL                              : subscription-manager repos --enable codeready-builder-for-rhel-<version>-\$(uname -m)-rpms"
                fi
                echo ""
            fi
            if [ $need_go -eq 1 ] && [ ${#missing_pkgs[@]} -gt 0 ]; then
                echo "dnf install -y golang ${missing_pkgs[*]}"
            elif [ $need_go -eq 1 ]; then
                echo "dnf install -y golang"
            else
                echo "dnf install -y ${missing_pkgs[*]}"
            fi
        } > "$fix_script"
        chmod +x "$fix_script"
        echo "A root fix script has been created. Run it as 'root' user :"
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

    # Ensure libstdc++.so is reachable for CGO linking.
    # Go invokes gcc (not g++) as the linker driver, so GCC does not
    # automatically add its C++ library search paths. We create an
    # unversioned symlink in .cgolibs and expose it via CGO_LDFLAGS.
    local libstdcxx
    libstdcxx=$(ldconfig -p 2>/dev/null | grep "libstdc++\.so\." | awk '{print $NF}' | head -1)
    if [ -n "$libstdcxx" ]; then
        mkdir -p "$BUILD_DIR/.cgolibs"
        ln -sf "$libstdcxx" "$BUILD_DIR/.cgolibs/libstdc++.so" 2>/dev/null || true
        export CGO_LDFLAGS="-L$(realpath "$BUILD_DIR/.cgolibs") ${CGO_LDFLAGS}"
    fi

    # Build the application with injected variables
    echo "Compiling ..."
    if go build -v -ldflags "-X main.AppName=$BINARY_NAME -X 'main.AppDisplayName=$APP_DISPLAY' -X main.AppVersion=$APP_VER -X main.AppVersionDate=$APP_DATE -X main.EncryptConnectionDetails=$ENCRYPT_CONN" -o "$BINARY_NAME" "$BUILD_DIR"; then
        echo ""
        print_header "Build Successful!"
        echo ""
        ls -lh "$BINARY_NAME"
        echo ""

        # Write build info file
        BUILD_INFO_FILE="${BINARY_NAME}_BuildInfo_Linux.txt"
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
            BINARY_BYTES=$(stat -c '%s' "$BINARY_NAME")
            BINARY_HUMAN=$(du -sh "$BINARY_NAME" | awk '{print $1}')
            echo "Binary Size      : $BINARY_BYTES bytes ($BINARY_HUMAN)"
            echo ""
            echo "OS               : $(uname -s)"
            echo "OS Version       : $(uname -r)"
            echo "Architecture     : $(uname -m)"
            if [ -f /etc/os-release ]; then
                echo "Distribution     : $(grep '^PRETTY_NAME=' /etc/os-release | cut -d'=' -f2 | tr -d '"')"
            fi
            echo ""
            echo "Go Version       : $(go version)"
            echo ""
            echo "gocql            : ${VER_GOCQL:-not found}"
            echo "go-duckdb        : ${VER_DUCKDB:-not found}"
            echo "parquet-go       : ${VER_PARQUET:-not found}"
            echo ""
            echo "MD5              : $(md5sum "$BINARY_NAME" | awk '{print $1}')"
            echo "SHA256           : $(sha256sum "$BINARY_NAME" | awk '{print $1}')"
            echo "SHA512           : $(sha512sum "$BINARY_NAME" | awk '{print $1}')"
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

    if [ -f "${BINARY_NAME}_BuildInfo_Linux.txt" ]; then
        echo "Removing build info file : ${BINARY_NAME}_BuildInfo_Linux.txt"
        if rm -f "${BINARY_NAME}_BuildInfo_Linux.txt"; then
            cleaned=1
        else
            print_error "Failed to remove ${BINARY_NAME}_BuildInfo_Linux.txt"
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


