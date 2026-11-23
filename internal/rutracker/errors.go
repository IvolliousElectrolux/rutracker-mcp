package rutracker

import "errors"

var (
	// ErrMirror means the current mirror could not be reached or returned 5xx.
	ErrMirror = errors.New("rutracker mirror is unavailable")
	// ErrSession means the site sent the browser to the login page or returned a non-torrent body.
	ErrSession = errors.New("rutracker session is missing or expired")
	// ErrCaptcha means automatic captcha recognition did not produce a session.
	ErrCaptcha = errors.New("login requires a captcha")
	// ErrLogin means the site rejected the username and password.
	ErrLogin = errors.New("login was rejected")
	// ErrNoCredentials means this call has neither a session cookie nor a password.
	ErrNoCredentials = errors.New("no credentials")
	// ErrNotFound means the topic or its file list is missing.
	ErrNotFound = errors.New("topic was not found")
	// ErrTooLarge means the download exceeded 32MB.
	ErrTooLarge = errors.New("torrent is larger than 32MB")
	// ErrBaseURL means RUTRACKER_BASE_URL is not an https URL.
	ErrBaseURL = errors.New("base URL must be an https URL with a host")
	// ErrBadName means a download file name is not safe to join onto the download directory.
	ErrBadName = errors.New("download file name is not safe")
)
