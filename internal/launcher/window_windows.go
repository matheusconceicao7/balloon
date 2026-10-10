package launcher

import (
	"fmt"
	"net/http"
	"runtime"
	"syscall"
	"unsafe"
)

var (
	user32           = syscall.NewLazyDLL("user32.dll")
	gdi32            = syscall.NewLazyDLL("gdi32.dll")
	kernel32         = syscall.NewLazyDLL("kernel32.dll")
	shell32          = syscall.NewLazyDLL("shell32.dll")
	registerClass    = user32.NewProc("RegisterClassExW")
	unregisterClass  = user32.NewProc("UnregisterClassW")
	createWindow     = user32.NewProc("CreateWindowExW")
	defWindowProc    = user32.NewProc("DefWindowProcW")
	destroyWindow    = user32.NewProc("DestroyWindow")
	showWindow       = user32.NewProc("ShowWindow")
	updateWindow     = user32.NewProc("UpdateWindow")
	getMessage       = user32.NewProc("GetMessageW")
	translateMessage = user32.NewProc("TranslateMessage")
	dispatchMessage  = user32.NewProc("DispatchMessageW")
	isDialogMessage  = user32.NewProc("IsDialogMessageW")
	postQuitMessage  = user32.NewProc("PostQuitMessage")
	sendMessage      = user32.NewProc("SendMessageW")
	setWindowText    = user32.NewProc("SetWindowTextW")
	getWindowText    = user32.NewProc("GetWindowTextW")
	enableWindow     = user32.NewProc("EnableWindow")
	setFocus         = user32.NewProc("SetFocus")
	loadCursor       = user32.NewProc("LoadCursorW")
	loadImage        = user32.NewProc("LoadImageW")
	destroyIcon      = user32.NewProc("DestroyIcon")
	adjustWindowRect = user32.NewProc("AdjustWindowRectEx")
	setTimer         = user32.NewProc("SetTimer")
	killTimer        = user32.NewProc("KillTimer")
	messageBox       = user32.NewProc("MessageBoxW")
)

// These structures match Win32's pointer-sized handles and fixed-width fields.
type windowClass struct {
	Size, Style                        uint32
	WindowProc                         uintptr
	ClassExtra, WindowExtra            int32
	Instance, Icon, Cursor, Background uintptr
	MenuName, ClassName                *uint16
	SmallIcon                          uintptr
}

type windowMessage struct {
	Window         uintptr
	Message        uint32
	WParam, LParam uintptr
	Time           uint32
	X, Y           int32
	Private        uint32
}

type windowRect struct{ Left, Top, Right, Bottom int32 }

const (
	wmDestroy = 0x0002
	wmClose   = 0x0010
	wmCommand = 0x0111
	wmTimer   = 0x0113
	wmSetFont = 0x0030
	wsChild   = 0x40000000
	wsVisible = 0x10000000
	wsTabStop = 0x00010000
	// Caption, system menu and minimize button; the small launcher has no resize grip.
	launcherStyle = 0x00C00000 | 0x00080000 | 0x00020000
	startID       = 1001
	stopID        = 1002
	browserID     = 1003
)

type nativeWindow struct {
	controller                              *Controller
	handle, instance, font                  uintptr
	port, start, stop, browser, status, url uintptr
	dpi                                     int
}

func wide(s string) *uint16 { return syscall.StringToUTF16Ptr(s) }

// Run displays a native launcher. The HTTP server lives in this same process.
func Run(handler http.Handler) error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	// ShellExecute may delegate URL opening to COM shell extensions.
	ole32 := syscall.NewLazyDLL("ole32.dll")
	if result, _, _ := ole32.NewProc("CoInitializeEx").Call(0, 0x6); int32(result) < 0 {
		return fmt.Errorf("initialize Windows browser integration: 0x%08x", uint32(result))
	}
	defer ole32.NewProc("CoUninitialize").Call()
	user32.NewProc("SetProcessDPIAware").Call()
	w := &nativeWindow{controller: New(handler, openDefaultBrowser), dpi: 96}
	defer w.controller.Stop()
	getDPI := user32.NewProc("GetDpiForSystem")
	if getDPI.Find() == nil {
		if dpi, _, _ := getDPI.Call(); dpi != 0 {
			w.dpi = int(dpi)
		}
	}
	w.instance, _, _ = kernel32.NewProc("GetModuleHandleW").Call(0)
	cursor, _, _ := loadCursor.Call(0, 32512) // IDC_ARROW
	// APP is the icon group embedded in both Windows architecture builds.
	icon, _, err := loadImage.Call(w.instance, uintptr(unsafe.Pointer(wide("APP"))),
		1, uintptr(w.scale(32)), uintptr(w.scale(32)), 0) // IMAGE_ICON
	if icon == 0 {
		return fmt.Errorf("load Balloon application icon: %w", err)
	}
	defer destroyIcon.Call(icon)
	smallIcon, _, err := loadImage.Call(w.instance, uintptr(unsafe.Pointer(wide("APP"))),
		1, uintptr(w.scale(16)), uintptr(w.scale(16)), 0)
	if smallIcon == 0 {
		return fmt.Errorf("load Balloon window icon: %w", err)
	}
	defer destroyIcon.Call(smallIcon)
	className := wide("BalloonDesktopLauncher")
	class := windowClass{
		WindowProc: syscall.NewCallback(w.windowProc), Instance: w.instance,
		Cursor: cursor, Background: 16, ClassName: className, // COLOR_BTNFACE + 1
		Icon: icon, SmallIcon: smallIcon,
	}
	class.Size = uint32(unsafe.Sizeof(class))
	if atom, _, err := registerClass.Call(uintptr(unsafe.Pointer(&class))); atom == 0 {
		return fmt.Errorf("create launcher window class: %w", err)
	}
	defer func() { unregisterClass.Call(uintptr(unsafe.Pointer(className)), w.instance) }()
	fontHeight := -int32(w.scale(16))
	w.font, _, _ = gdi32.NewProc("CreateFontW").Call(
		uintptr(fontHeight), 0, 0, 0, 400, 0, 0, 0, 1, 0, 0, 0, 0,
		uintptr(unsafe.Pointer(wide("Segoe UI"))),
	)
	if w.font == 0 {
		return fmt.Errorf("create launcher font")
	}
	defer gdi32.NewProc("DeleteObject").Call(w.font)
	rect := windowRect{Right: int32(w.scale(540)), Bottom: int32(w.scale(280))}
	adjustWindowRect.Call(uintptr(unsafe.Pointer(&rect)), launcherStyle, 0, 0x10000)
	handle, _, err := createWindow.Call(
		0x10000, uintptr(unsafe.Pointer(className)), uintptr(unsafe.Pointer(wide("Balloon"))),
		launcherStyle, 0x80000000, 0x80000000,
		uintptr(rect.Right-rect.Left), uintptr(rect.Bottom-rect.Top), 0, 0, w.instance, 0,
	)
	if handle == 0 {
		return fmt.Errorf("create launcher window: %w", err)
	}
	w.handle = handle
	defer destroyWindow.Call(handle)
	if err := w.createControls(); err != nil {
		return err
	}
	w.updateState()
	showWindow.Call(handle, 1)
	updateWindow.Call(handle)
	setFocus.Call(w.port)
	if timer, _, err := setTimer.Call(handle, 1, 500, 0); timer == 0 {
		return fmt.Errorf("create launcher status timer: %w", err)
	}
	var msg windowMessage
	for {
		result, _, err := getMessage.Call(uintptr(unsafe.Pointer(&msg)), 0, 0, 0)
		if int32(result) == -1 {
			return fmt.Errorf("read launcher events: %w", err)
		}
		if result == 0 {
			return nil
		}
		if handled, _, _ := isDialogMessage.Call(handle, uintptr(unsafe.Pointer(&msg))); handled == 0 {
			translateMessage.Call(uintptr(unsafe.Pointer(&msg)))
			dispatchMessage.Call(uintptr(unsafe.Pointer(&msg)))
		}
	}
}

func (w *nativeWindow) scale(n int) int { return n * w.dpi / 96 }

func (w *nativeWindow) createControls() error {
	controls := []struct {
		class, text         string
		style               uintptr
		x, y, width, height int
		id                  uintptr
		out                 *uintptr
	}{
		{"STATIC", "Start Balloon on this computer.", 0, 24, 20, 492, 24, 0, nil},
		{"STATIC", "Port:", 0, 24, 65, 44, 24, 0, nil},
		{"EDIT", "8080", wsTabStop | 0x00800000 | 0x2000, 72, 60, 100, 28, 1004, &w.port},
		{"BUTTON", "Start server", wsTabStop | 1, 24, 104, 112, 32, startID, &w.start},
		{"BUTTON", "Stop server", wsTabStop, 148, 104, 112, 32, stopID, &w.stop},
		{"BUTTON", "Open browser", wsTabStop, 272, 104, 132, 32, browserID, &w.browser},
		{"STATIC", "Server stopped.", 0, 24, 152, 492, 40, 0, &w.status},
		{"EDIT", "", wsTabStop | 0x00800000 | 0x0800 | 0x0080, 24, 198, 492, 28, 1005, &w.url},
		{"STATIC", "Keep this window open while working. Closing it stops the server.", 0, 24, 240, 492, 32, 0, nil},
	}
	for _, c := range controls {
		handle, _, err := createWindow.Call(0,
			uintptr(unsafe.Pointer(wide(c.class))), uintptr(unsafe.Pointer(wide(c.text))),
			wsChild|wsVisible|c.style,
			uintptr(w.scale(c.x)), uintptr(w.scale(c.y)), uintptr(w.scale(c.width)), uintptr(w.scale(c.height)),
			w.handle, c.id, w.instance, 0,
		)
		if handle == 0 {
			return fmt.Errorf("create %s control: %w", c.text, err)
		}
		if c.out != nil {
			*c.out = handle
		}
		sendMessage.Call(handle, wmSetFont, w.font, 1)
	}
	sendMessage.Call(w.port, 0x00C5, 5, 0) // EM_LIMITTEXT
	return nil
}

func (w *nativeWindow) updateState() {
	running := w.controller.URL() != ""
	setEnabled(w.port, !running)
	setEnabled(w.start, !running)
	setEnabled(w.stop, running)
	setEnabled(w.browser, running)
	setText(w.url, w.controller.URL())
	if running {
		setText(w.status, "Server running.")
	} else {
		setText(w.status, "Server stopped.")
	}
}

func setText(handle uintptr, text string) {
	setWindowText.Call(handle, uintptr(unsafe.Pointer(wide(text))))
}

func setEnabled(handle uintptr, enabled bool) {
	var value uintptr
	if enabled {
		value = 1
	}
	enableWindow.Call(handle, value)
}

func (w *nativeWindow) windowProc(handle uintptr, message uint32, wparam, lparam uintptr) uintptr {
	switch message {
	case wmCommand:
		if wparam>>16 != 0 {
			break
		} // BN_CLICKED only
		id := wparam & 0xffff
		// IsDialogMessage sends IDOK when Enter is pressed in an edit control.
		// Start when stopped, or reopen the browser when already running.
		if id == 1 {
			id = startID
			if w.controller.URL() != "" {
				id = browserID
			}
		}
		var err error
		switch id {
		case startID:
			var text [16]uint16
			getWindowText.Call(w.port, uintptr(unsafe.Pointer(&text[0])), uintptr(len(text)))
			err = w.controller.Start(syscall.UTF16ToString(text[:]))
		case stopID:
			err = w.controller.Stop()
		case browserID:
			err = w.controller.OpenBrowser()
		default:
			// Escape's IDCANCEL is intentionally ignored: it must not stop a
			// running server while the user is working in the browser.
			return 0
		}
		w.updateState()
		if err != nil {
			showError(handle, err)
		}
		if w.controller.URL() == "" {
			setFocus.Call(w.port)
		} else {
			setFocus.Call(w.browser)
		}
		return 0
	case wmTimer:
		if err := w.controller.PollError(); err != nil {
			w.updateState()
			showError(handle, err)
		}
		return 0
	case wmClose:
		w.controller.Stop()
		destroyWindow.Call(handle)
		return 0
	case wmDestroy:
		killTimer.Call(handle, 1)
		postQuitMessage.Call(0)
		return 0
	}
	result, _, _ := defWindowProc.Call(handle, uintptr(message), wparam, lparam)
	return result
}

func openDefaultBrowser(url string) error {
	result, _, _ := shell32.NewProc("ShellExecuteW").Call(0,
		uintptr(unsafe.Pointer(wide("open"))), uintptr(unsafe.Pointer(wide(url))), 0, 0, 1,
	)
	if result <= 32 {
		return fmt.Errorf("Windows browser error %d", result)
	}
	return nil
}

func showError(owner uintptr, err error) {
	messageBox.Call(owner, uintptr(unsafe.Pointer(wide(err.Error()))), uintptr(unsafe.Pointer(wide("Balloon"))), 0x10)
}

// ShowError reports startup failures when there is no launcher window yet.
func ShowError(err error) { showError(0, err) }
