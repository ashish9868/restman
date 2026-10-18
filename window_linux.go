//go:build linux && !browser

package main

/*
#cgo pkg-config: gtk+-3.0 webkit2gtk-4.1
#include <gtk/gtk.h>
#include <stdlib.h>
#include <string.h>
#include <webkit2/webkit2.h>

static GtkWidget *app_window;
static WebKitWebView *app_view;

static void set_title(const char *title) {
	if (app_window) gtk_window_set_title(GTK_WINDOW(app_window), title);
}

static void set_zoom(double level) {
	if (app_view) webkit_web_view_set_zoom_level(app_view, level);
}

static void quit_app(GtkWidget *widget, gpointer data) {
	(void)widget;
	(void)data;
	gtk_main_quit();
}

typedef struct {
	char *path;
	int done;
	GMutex mutex;
	GCond cond;
} pick_req;

static gboolean pick_idle(gpointer data) {
	pick_req *req = data;
	GtkWidget *dialog = gtk_file_chooser_dialog_new(
		"Select file", GTK_WINDOW(app_window), GTK_FILE_CHOOSER_ACTION_OPEN,
		"Cancel", GTK_RESPONSE_CANCEL, "Select", GTK_RESPONSE_ACCEPT, NULL);
	if (gtk_dialog_run(GTK_DIALOG(dialog)) == GTK_RESPONSE_ACCEPT) {
		req->path = gtk_file_chooser_get_filename(GTK_FILE_CHOOSER(dialog));
	}
	gtk_widget_destroy(dialog);
	g_mutex_lock(&req->mutex);
	req->done = 1;
	g_cond_signal(&req->cond);
	g_mutex_unlock(&req->mutex);
	return G_SOURCE_REMOVE;
}

static gboolean save_idle(gpointer data) {
	pick_req *req = data;
	GtkWidget *dialog = gtk_file_chooser_dialog_new(
		"Save file", GTK_WINDOW(app_window), GTK_FILE_CHOOSER_ACTION_SAVE,
		"Cancel", GTK_RESPONSE_CANCEL, "Save", GTK_RESPONSE_ACCEPT, NULL);
	gtk_file_chooser_set_do_overwrite_confirmation(GTK_FILE_CHOOSER(dialog), TRUE);
	if (req->path && req->path[0]) {
		gtk_file_chooser_set_current_name(GTK_FILE_CHOOSER(dialog), req->path);
		req->path = NULL;
	}
	GtkFileFilter *filter = gtk_file_filter_new();
	gtk_file_filter_set_name(filter, "JSON");
	gtk_file_filter_add_pattern(filter, "*.json");
	gtk_file_chooser_add_filter(GTK_FILE_CHOOSER(dialog), filter);
	if (gtk_dialog_run(GTK_DIALOG(dialog)) == GTK_RESPONSE_ACCEPT) {
		req->path = gtk_file_chooser_get_filename(GTK_FILE_CHOOSER(dialog));
	}
	gtk_widget_destroy(dialog);
	g_mutex_lock(&req->mutex);
	req->done = 1;
	g_cond_signal(&req->cond);
	g_mutex_unlock(&req->mutex);
	return G_SOURCE_REMOVE;
}

static char *save_file(const char *name) {
	pick_req req;
	memset(&req, 0, sizeof(req));
	req.path = (char *)name;
	g_mutex_init(&req.mutex);
	g_cond_init(&req.cond);
	g_idle_add(save_idle, &req);
	g_mutex_lock(&req.mutex);
	while (!req.done) {
		g_cond_wait(&req.cond, &req.mutex);
	}
	g_mutex_unlock(&req.mutex);
	g_mutex_clear(&req.mutex);
	g_cond_clear(&req.cond);
	return req.path;
}

static char *pick_file(void) {
	pick_req req;
	memset(&req, 0, sizeof(req));
	g_mutex_init(&req.mutex);
	g_cond_init(&req.cond);
	g_idle_add(pick_idle, &req);
	g_mutex_lock(&req.mutex);
	while (!req.done) {
		g_cond_wait(&req.cond, &req.mutex);
	}
	g_mutex_unlock(&req.mutex);
	g_mutex_clear(&req.mutex);
	g_cond_clear(&req.cond);
	return req.path;
}

static void open_window(const char *url) {
	gtk_init(NULL, NULL);
	GtkWidget *window = gtk_window_new(GTK_WINDOW_TOPLEVEL);
	gtk_window_set_title(GTK_WINDOW(window), "Restman");
	gtk_window_set_default_size(GTK_WINDOW(window), 1440, 900);
	gtk_window_set_position(GTK_WINDOW(window), GTK_WIN_POS_CENTER);
	app_window = window;
	GtkWidget *view = webkit_web_view_new();
	app_view = WEBKIT_WEB_VIEW(view);
	gtk_container_add(GTK_CONTAINER(window), view);
	webkit_web_view_load_uri(WEBKIT_WEB_VIEW(view), url);
	g_signal_connect_data(window, "destroy", G_CALLBACK(quit_app), NULL, NULL, 0);
	gtk_widget_show_all(window);
	gtk_main();
}
*/
import "C"
import (
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"unsafe"
)

func serve(listener net.Listener, handler http.Handler, page string) error {
	if os.Getenv("DISPLAY") == "" && os.Getenv("WAYLAND_DISPLAY") == "" {
		fmt.Println("No display found. Open " + page)
		return http.Serve(listener, handler)
	}
	go func() {
		if err := http.Serve(listener, handler); err != nil {
			log.Fatal(err)
		}
	}()
	fmt.Println("Opening Restman")
	target := C.CString(page)
	defer C.free(unsafe.Pointer(target))
	C.open_window(target)
	return nil
}

func setWindowTitle(title string) {
	if title == "" {
		title = "Restman"
	}
	value := C.CString(title)
	defer C.free(unsafe.Pointer(value))
	C.set_title(value)
}

func setZoom(level float64) {
	if level < 0.5 {
		level = 0.5
	}
	if level > 2 {
		level = 2
	}
	C.set_zoom(C.double(level))
}

func saveFile(name string) (string, error) {
	suggested := C.CString(name)
	path := C.save_file(suggested)
	C.free(unsafe.Pointer(suggested))
	if path == nil {
		return "", nil
	}
	defer C.free(unsafe.Pointer(path))
	return C.GoString(path), nil
}

func pickFile() (string, error) {
	path := C.pick_file()
	if path == nil {
		return "", nil
	}
	defer C.free(unsafe.Pointer(path))
	return C.GoString(path), nil
}
