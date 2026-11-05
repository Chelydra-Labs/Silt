//go:build linux && !gtk3

package main

// Native text/html read from the GTK4 clipboard.
//
// GDK's clipboard API is async-only: the data arrives from the clipboard owner
// through the default GMainContext, so it can only be consumed by pumping that
// context on the GTK main thread. This follows Wails' own clipboard read shape
// (pkg/application/linux_cgo.c, clipboard_get_text_sync): start the async read,
// then iterate the default context until the callback lands. Two deliberate
// differences: the wait is time-bounded (a hung clipboard owner must not freeze
// the UI thread forever) and the call state lives on the heap so an abandoned
// read can never write into a dead stack frame.

/*
#cgo pkg-config: gtk4

#include <gtk/gtk.h>
#include <gdk/gdk.h>

#define SILT_CLIPBOARD_HTML_TIMEOUT_US (1000 * 1000) // 1s
#define SILT_CLIPBOARD_HTML_GRACE_US (250 * 1000)    // post-cancel drain
#define SILT_CLIPBOARD_HTML_MAX_BYTES (4u * 1024u * 1024u)
#define SILT_CLIPBOARD_HTML_CHUNK (64u * 1024u)

typedef struct {
    GByteArray *buffer;
    GInputStream *stream;
    GCancellable *cancellable;
    GError *error;
    gboolean done;
} SiltClipboardHTMLRead;

static void silt_clipboard_html_free_state(SiltClipboardHTMLRead *state) {
    if (state->buffer != NULL) g_byte_array_unref(state->buffer);
    if (state->stream != NULL) g_object_unref(state->stream);
    if (state->cancellable != NULL) g_object_unref(state->cancellable);
    if (state->error != NULL) g_error_free(state->error);
    g_free(state);
}

static void silt_clipboard_html_read_chunk(SiltClipboardHTMLRead *state);

static void silt_clipboard_html_chunk_cb(GObject *source, GAsyncResult *result, gpointer user_data) {
    SiltClipboardHTMLRead *state = (SiltClipboardHTMLRead *)user_data;
    GError *error = NULL;
    GBytes *bytes = g_input_stream_read_bytes_finish(G_INPUT_STREAM(source), result, &error);

    if (bytes == NULL) {
        state->error = error;
        state->done = TRUE;
        return;
    }

    gsize size = 0;
    const guint8 *data = g_bytes_get_data(bytes, &size);
    if (size == 0) {
        g_bytes_unref(bytes); // EOF
        state->done = TRUE;
        return;
    }
    if (state->buffer->len + size > SILT_CLIPBOARD_HTML_MAX_BYTES) {
        g_bytes_unref(bytes);
        state->error = g_error_new_literal(G_IO_ERROR, G_IO_ERROR_NO_SPACE,
                                           "clipboard HTML exceeds the size limit");
        state->done = TRUE;
        return;
    }
    g_byte_array_append(state->buffer, data, (guint)size);
    g_bytes_unref(bytes);
    silt_clipboard_html_read_chunk(state);
}

static void silt_clipboard_html_read_chunk(SiltClipboardHTMLRead *state) {
    g_input_stream_read_bytes_async(state->stream,
                                    SILT_CLIPBOARD_HTML_CHUNK,
                                    G_PRIORITY_DEFAULT,
                                    state->cancellable,
                                    silt_clipboard_html_chunk_cb,
                                    state);
}

static void silt_clipboard_html_read_cb(GObject *source, GAsyncResult *result, gpointer user_data) {
    SiltClipboardHTMLRead *state = (SiltClipboardHTMLRead *)user_data;
    const char *mime_type = NULL;
    GError *error = NULL;

    state->stream = gdk_clipboard_read_finish(GDK_CLIPBOARD(source), result, &mime_type, &error);
    if (state->stream == NULL) {
        state->error = error;
        state->done = TRUE;
        return;
    }
    silt_clipboard_html_read_chunk(state);
}

// The clipboard's HTML targets, NULL-terminated, or NULL when it advertises
// none. Prefix match because owners may offer "text/html;charset=utf-8" rather
// than bare "text/html"; GDK then matches formats by exact interned string, so
// the read requests the advertised spellings instead of a fixed "text/html".
// The strings belong to the clipboard's formats; the async read interns its
// MIME list, so g_free the array as soon as it returns.
static const char **silt_clipboard_html_targets(GdkClipboard *clipboard) {
    GdkContentFormats *formats = gdk_clipboard_get_formats(clipboard);
    if (formats == NULL) return NULL;

    gsize count = 0;
    const char *const *mime_types = gdk_content_formats_get_mime_types(formats, &count);
    const char **targets = g_new0(const char *, count + 1);
    gsize target_count = 0;
    for (gsize i = 0; i < count; i++) {
        if (g_str_has_prefix(mime_types[i], "text/html")) targets[target_count++] = mime_types[i];
    }
    if (target_count == 0) {
        g_free(targets);
        return NULL;
    }
    return targets;
}

// Reads the clipboard's HTML. Returns a newly allocated UTF-8 string, or NULL
// when the clipboard holds no HTML (also the empty-clipboard case). On failure
// returns NULL and stores a newly allocated message in *error_out.
char *silt_clipboard_read_html_sync(char **error_out) {
    if (error_out != NULL) *error_out = NULL;

    GdkDisplay *display = gdk_display_get_default();
    if (display == NULL) {
        if (error_out != NULL) *error_out = g_strdup("clipboard read requires an open GTK display");
        return NULL;
    }

    GdkClipboard *clipboard = gdk_display_get_clipboard(display);
    const char **targets = silt_clipboard_html_targets(clipboard);
    if (targets == NULL) return NULL;

    SiltClipboardHTMLRead *state = g_new0(SiltClipboardHTMLRead, 1);
    state->buffer = g_byte_array_new();
    state->cancellable = g_cancellable_new();

    gdk_clipboard_read_async(clipboard, targets, G_PRIORITY_DEFAULT,
                             state->cancellable, silt_clipboard_html_read_cb, state);
    g_free(targets);

    GMainContext *context = g_main_context_default();
    gint64 deadline = g_get_monotonic_time() + SILT_CLIPBOARD_HTML_TIMEOUT_US;
    while (!state->done && g_get_monotonic_time() < deadline) {
        // Non-blocking iteration so the deadline is actually observed; the
        // 1ms sleep keeps a waiting read from spinning a core.
        if (!g_main_context_iteration(context, FALSE)) g_usleep(1000);
    }

    if (!state->done) {
        g_cancellable_cancel(state->cancellable);
        gint64 grace = g_get_monotonic_time() + SILT_CLIPBOARD_HTML_GRACE_US;
        while (!state->done && g_get_monotonic_time() < grace) {
            if (!g_main_context_iteration(context, FALSE)) g_usleep(1000);
        }
    }

    if (!state->done) {
        // The cancelled read still has not reported back. Freeing the state
        // would let a late callback write into freed memory, so abandon it
        // (one bounded leak) rather than risk a crash.
        if (error_out != NULL) *error_out = g_strdup("clipboard HTML read timed out");
        return NULL;
    }

    // A read that completed inside the grace window has a whole buffer even
    // though the deadline passed, so only the error field decides the outcome.
    char *html = NULL;
    if (state->error != NULL) {
        if (error_out != NULL) *error_out = g_strdup(state->error->message);
    } else if (state->buffer->len > 0) {
        gchar *raw = g_strndup((const gchar *)state->buffer->data, state->buffer->len);
        if (g_utf8_validate(raw, -1, NULL)) {
            html = raw;
        } else {
            // One bad byte should not cost the user their rich paste; repair it
            // the way an HTML parser would.
            html = g_utf8_make_valid(raw, -1);
            g_free(raw);
        }
    }

    silt_clipboard_html_free_state(state);
    return html;
}

void silt_clipboard_html_free_string(char *text) {
    if (text != NULL) g_free(text);
}
*/
import "C"

import (
	"errors"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// readHTMLClipboard reads the clipboard's text/html target on the GTK main
// thread. Bound methods run on their own goroutine, while GDK clipboard
// callbacks are delivered by the GTK main context, which only the main thread
// iterates.
func readHTMLClipboard() (string, error) {
	// Dispatch needs a live application to hop through; before wailsApp.Run
	// there is no GTK main thread to read from.
	if application.Get() == nil {
		return "", errors.New("clipboard HTML read requires a running application")
	}
	return application.InvokeSyncWithResultAndError(readHTMLClipboardMainThread)
}

func readHTMLClipboardMainThread() (string, error) {
	var cErr *C.char
	cHTML := C.silt_clipboard_read_html_sync(&cErr)
	if cErr != nil {
		message := C.GoString(cErr)
		C.silt_clipboard_html_free_string(cErr)
		return "", errors.New(message)
	}
	if cHTML == nil {
		return "", nil
	}
	defer C.silt_clipboard_html_free_string(cHTML)
	return C.GoString(cHTML), nil
}
