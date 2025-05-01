package client

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

// ErrBadResponse means that qbittorrent sent back an unexpected response
var ErrBadResponse = errors.New("received bad response")

// Client creates a connection to qbittorrent and performs requests
type Client struct {
	http          *http.Client
	URL           string
	Authenticated bool
	Jar           http.CookieJar
}

// NewClient creates a new client connection to qbittorrent.
// If requestTimeout sett to 0, defaults to 1 minute
func NewClient(url string, requestTimeout time.Duration) *Client {
	client := &Client{}

	// ensure url ends with "/"
	if url[len(url)-1:] != "/" {
		url += "/"
	}

	client.URL = url

	if requestTimeout == 0 {
		requestTimeout = time.Minute
	}

	// create cookie jar
	client.Jar, _ = cookiejar.New(nil)
	client.http = &http.Client{
		Jar:     client.Jar,
		Timeout: requestTimeout,
	}
	return client
}

// get will perform a GET request with no parameters
func (client *Client) get(endpoint string, opts map[string]string) (*http.Response, error) {
	req, err := http.NewRequest(http.MethodGet, client.URL+endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to build request: %w", err)
	}

	//add user-agent header to allow qbittorrent to identify us
	req.Header.Set("User-Agent", "go-qbittorrent v0.1")

	//add optional parameters that the user wants
	if len(opts) > 0 {
		query := req.URL.Query()
		for k, v := range opts {
			query.Add(k, v)
		}
		req.URL.RawQuery = query.Encode()
	}

	resp, err := client.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to perform %s request: %w", endpoint, err)
	}

	return resp, nil
}

func (client *Client) getStatus(endpoint string, opts map[string]string) (err error) {
	var resp *http.Response
	resp, err = client.get(endpoint, opts)
	if err != nil {
		return
	}
	if resp.StatusCode != http.StatusOK {
		err = ErrBadResponse
	}
	_ = resp.Body.Close()
	return
}

func (client *Client) getJSON(endpoint string, opts map[string]string, readVal any) (err error) {
	var resp *http.Response
	resp, err = client.get(endpoint, opts)
	if err != nil {
		return
	}
	if resp.StatusCode == http.StatusOK {
		err = json.NewDecoder(resp.Body).Decode(readVal)
	} else {
		err = ErrBadResponse
	}
	_ = resp.Body.Close()
	return
}

// post will perform a POST request with no content-type specified
func (client *Client) post(endpoint string, opts map[string]string) (*http.Response, error) {
	// add optional parameters that the user wants
	form := url.Values{}
	for k, v := range opts {
		form.Add(k, v)
	}

	req, err := http.NewRequest(http.MethodPost, client.URL+endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, fmt.Errorf("failed to build request: %w", err)
	}

	// add the content-type so qbittorrent knows what to expect
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	// add user-agent header to allow qbittorrent to identify us
	req.Header.Set("User-Agent", "go-qbittorrent v0.1")

	resp, err := client.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to perform request: %w", err)
	}

	return resp, nil
}

func (client *Client) postStatus(endpoint string, opts map[string]string) (err error) {
	var resp *http.Response
	resp, err = client.post(endpoint, opts)
	if err != nil {
		return
	}
	if resp.StatusCode != http.StatusOK {
		err = ErrBadResponse
	}
	_ = resp.Body.Close()
	return
}

func (client *Client) postJSON(endpoint string, opts map[string]string, readVal any) (err error) {
	var resp *http.Response
	resp, err = client.post(endpoint, opts)
	if err != nil {
		return
	}
	if resp.StatusCode == http.StatusOK {
		err = json.NewDecoder(resp.Body).Decode(readVal)
	} else {
		err = ErrBadResponse
	}
	_ = resp.Body.Close()
	return
}

// postMultipart will perform a multiple part POST request
func (client *Client) postMultipart(endpoint string, buffer bytes.Buffer, contentType string) (
	resp *http.Response, err error,
) {
	req, err := http.NewRequest("POST", client.URL+endpoint, &buffer)
	if err != nil {
		return nil, fmt.Errorf("error creating request: %w", err)
	}

	// add the content-type so qbittorrent knows what to expect
	req.Header.Set("Content-Type", contentType)
	// add user-agent header to allow qbittorrent to identify us
	req.Header.Set("User-Agent", "go-qbittorrent v0.2")

	resp, err = client.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to perform request: %w", err)
	}

	return resp, nil
}

// writeOptions will write a map to the buffer through multipart.NewWriter
func writeOptions(writer *multipart.Writer, opts map[string]string) (err error) {
	for key, val := range opts {
		if err = writer.WriteField(key, val); err != nil {
			return
		}
	}
	return
}

// postMultipartData will perform a multiple part POST request without a file
func (client *Client) postMultipartData(endpoint string, opts map[string]string) (*http.Response, error) {
	var buffer bytes.Buffer
	writer := multipart.NewWriter(&buffer)

	// write the options to the buffer
	// will contain the link string
	if err := writeOptions(writer, opts); err != nil {
		return nil, fmt.Errorf("failed to write options: %w", err)
	}

	// close the writer before doing request to get closing line on multipart request
	if err := writer.Close(); err != nil {
		return nil, fmt.Errorf("failed to close writer: %w", err)
	}

	resp, err := client.postMultipart(endpoint, buffer, writer.FormDataContentType())
	if err != nil {
		return nil, err
	}

	return resp, nil
}

// postMultipartFile will perform a multiple part POST request with a file
func (client *Client) postMultipartReader(endpoint string, in io.Reader, opts map[string]string) (
	*http.Response, error,
) {
	var buffer bytes.Buffer
	writer := multipart.NewWriter(&buffer)

	// write the options to the buffer
	if err := writeOptions(writer, opts); err != nil {
		return nil, err
	}

	// create form for writing the file to and give it the filename
	formWriter, err := writer.CreateFormFile("torrents", "file.torrent")
	if err != nil {
		return nil, err
	}

	// copy the file contents into the form
	if _, err := io.Copy(formWriter, in); err != nil {
		return nil, err
	}

	// close the writer before doing request to get closing line on multipart request
	if err := writer.Close(); err != nil {
		return nil, err
	}

	resp, err := client.postMultipart(endpoint, buffer, writer.FormDataContentType())
	if err != nil {
		return nil, err
	}

	return resp, nil
}

// Application endpoints

// Login logs you in to the qbittorrent client
// returns the current authentication status
func (client *Client) Login(opts LoginOptions) (err error) {
	params := map[string]string{}

	if opts.Username != "" {
		params["username"] = opts.Username
	}
	if opts.Password != "" {
		params["password"] = opts.Password
	}

	var resp *http.Response
	resp, err = client.post("api/v2/auth/login", params)
	if err != nil {
		return
	} else {
		defer resp.Body.Close()
		if resp.StatusCode == 403 {
			return fmt.Errorf("user's IP is banned for too many failed login attempts")
		}
	}

	// add the cookie to cookie jar to authenticate later requests
	if cookies := resp.Cookies(); len(cookies) > 0 {
		cookieURL, _ := url.Parse("http://localhost:8080")
		client.Jar.SetCookies(cookieURL, cookies)
		// create a new client with the cookie jar and replace the old one
		// so that all our later requests are authenticated
		client.http = &http.Client{
			Jar: client.Jar,
		}
	} else {
		return fmt.Errorf("could not get cookie")
	}

	// change authentication status so we know were authenticated in later requests
	client.Authenticated = true

	return
}

// Logout logs you out of the qbittorrent client
// returns the current authentication status
func (client *Client) Logout() error {
	err := client.getStatus("api/v2/auth/logout", nil)
	if err == nil {
		client.Authenticated = false
	}
	return err
}

// ApplicationVersion of the qbittorrent client
func (client *Client) ApplicationVersion() (version string, err error) {
	var resp *http.Response
	resp, err = client.post("api/v2/app/version", nil)
	if err != nil {
		return version, err
	}
	defer resp.Body.Close()
	buf := new(strings.Builder)
	if _, err = io.Copy(buf, resp.Body); err == nil {
		version = buf.String()
	}
	return
}

// WebAPIVersion of the qbittorrent client
func (client *Client) WebAPIVersion() (version string, err error) {
	var resp *http.Response
	resp, err = client.post("api/v2/app/webapiVersion", nil)
	if err != nil {
		return version, err
	}
	defer resp.Body.Close()
	buf := new(strings.Builder)
	if _, err = io.Copy(buf, resp.Body); err == nil {
		version = buf.String()
	}
	return
}

// BuildInfo of the qbittorrent client
func (client *Client) BuildInfo() (buildInfo BuildInfo, err error) {
	err = client.getJSON("api/v2/app/buildInfo", nil, &buildInfo)
	return
}

// Preferences of the qbittorrent client
func (client *Client) Preferences() (prefs Preferences, err error) {
	err = client.getJSON("api/v2/app/preferences", nil, &prefs)
	return
}

// SetPreferences of the qbittorrent client
func (client *Client) SetPreferences() error {
	// fixme: no arguments
	return client.postStatus("api/v2/app/setPreferences", nil)
}

// DefaultSavePath of the qbittorrent client
func (client *Client) DefaultSavePath() (path string, err error) {
	var resp *http.Response
	resp, err = client.get("api/v2/app/defaultSavePath", nil)
	if err != nil {
		return
	}
	buf := new(strings.Builder)
	if _, err = io.Copy(buf, resp.Body); err == nil {
		path = buf.String()
	}
	_ = resp.Body.Close()
	return
}

// Shutdown shuts down the qbittorrent client
func (client *Client) Shutdown() (err error) {
	err = client.getStatus("api/v2/app/shutdown", nil)
	return
}

// Log Endpoints

// Logs of the qbittorrent client
func (client *Client) Logs(filters map[string]string) (logs []Log, err error) {
	err = client.getJSON("api/v2/log/main", filters, &logs)
	return
}

// PeerLogs of the qbittorrent client
func (client *Client) PeerLogs(filters map[string]string) (logs []PeerLog, err error) {
	err = client.getJSON("api/v2/log/peers", filters, &logs)
	return
}

// TODO: Sync Endpoints

// TODO: Transfer Endpoints

// Info returns info you usually see in qBt status bar.
func (client *Client) Info() (info Info, err error) {
	err = client.getJSON("api/v2/transfer/info", nil, &info)
	return
}

// AltSpeedLimitsEnabled returns info you usually see in qBt status bar.
func (client *Client) AltSpeedLimitsEnabled() (mode bool, err error) {
	var decoded int
	err = client.getJSON("api/v2/transfer/speedLimitsMode", nil, &decoded)
	mode = decoded == 1
	return
}

// ToggleAltSpeedLimits returns info you usually see in qBt status bar.
func (client *Client) ToggleAltSpeedLimits() error {
	return client.getStatus("api/v2/transfer/toggleSpeedLimitsMode", nil)
}

// DlLimit returns info you usually see in qBt status bar.
func (client *Client) DlLimit() (dlLimit int, err error) {
	err = client.getJSON("api/v2/transfer/downloadLimit", nil, &dlLimit)
	return
}

// SetDlLimit returns info you usually see in qBt status bar.
func (client *Client) SetDlLimit(limit int) error {
	return client.getStatus("api/v2/transfer/setDownloadLimit", map[string]string{"limit": strconv.Itoa(limit)})
}

// UlLimit returns info you usually see in qBt status bar.
func (client *Client) UlLimit() (ulLimit int, err error) {
	err = client.getJSON("api/v2/transfer/uploadLimit", nil, &ulLimit)
	return
}

// SetUlLimit returns info you usually see in qBt status bar.
func (client *Client) SetUlLimit(limit int) error {
	return client.getStatus("api/v2/transfer/setUploadLimit", map[string]string{"limit": strconv.Itoa(limit)})
}

// Torrents returns a list of all torrents in qbittorrent matching your filter
func (client *Client) Torrents(opts TorrentsOptions) (torrentList []TorrentInfo, err error) {
	params := map[string]string{}
	if opts.Filter != nil {
		params["filter"] = *opts.Filter
	}
	if opts.Category != nil {
		params["category"] = *opts.Category
	}
	if opts.Sort != nil {
		params["sort"] = *opts.Sort
	}
	if opts.Reverse != nil {
		params["reverse"] = strconv.FormatBool(*opts.Reverse)
	}
	if opts.Offset != nil {
		params["offset"] = strconv.Itoa(*opts.Offset)
	}
	if opts.Limit != nil {
		params["limit"] = strconv.Itoa(*opts.Limit)
	}
	if opts.Hashes != nil {
		params["hashes"] = strings.Join(opts.Hashes, "%0A")
	}
	err = client.getJSON("api/v2/torrents/info", params, &torrentList)
	return
}

// Torrent returns a specific torrent matching the hash
func (client *Client) Torrent(hash string) (torrent Torrent, err error) {
	err = client.getJSON("api/v2/torrents/properties", map[string]string{"hash": strings.ToLower(hash)}, &torrent)
	return
}

// TorrentTrackers returns all trackers for a specific torrent matching the hash
func (client *Client) TorrentTrackers(hash string) (trackers []Tracker, err error) {
	err = client.getJSON("api/v2/torrents/trackers", map[string]string{"hash": strings.ToLower(hash)}, &trackers)
	return
}

// TorrentWebSeeds returns seeders for a specific torrent matching the hash
func (client *Client) TorrentWebSeeds(hash string) (webSeeds []WebSeed, err error) {
	err = client.getJSON("api/v2/torrents/webseeds", map[string]string{"hash": strings.ToLower(hash)}, &webSeeds)
	return
}

// TorrentFiles from given hash
func (client *Client) TorrentFiles(hash string) (files []TorrentFile, err error) {
	err = client.getJSON("api/v2/torrents/files", map[string]string{"hash": strings.ToLower(hash)}, &files)
	return
}

// TorrentPieceStates for all pieces of torrent
func (client *Client) TorrentPieceStates(hash string) (states []int, err error) {
	err = client.getJSON("api/v2/torrents/pieceStates", map[string]string{"hash": strings.ToLower(hash)}, &states)
	return
}

// TorrentPieceHashes for all pieces of torrent
func (client *Client) TorrentPieceHashes(hash string) (hashes []string, err error) {
	err = client.getJSON("api/v2/torrents/pieceHashes", map[string]string{"hash": strings.ToLower(hash)}, &hashes)
	return
}

// Pause torrents
func (client *Client) Pause(hashes []string) error {
	return client.getStatus("api/v2/torrents/pause", map[string]string{"hashes": strings.Join(hashes, "|")})
}

// Resume torrents
func (client *Client) Resume(hashes []string) error {
	return client.getStatus("api/v2/torrents/start", map[string]string{"hashes": strings.Join(hashes, "|")})
}

// Delete torrents and optionally delete their files
func (client *Client) Delete(hashes []string, deleteFiles bool) error {
	return client.getStatus("api/v2/torrents/delete",
		map[string]string{"hashes": strings.Join(hashes, "|"), "deleteFiles": strconv.FormatBool(deleteFiles)})
}

// Recheck torrents
func (client *Client) Recheck(hashes []string) error {
	return client.getStatus("api/v2/torrents/recheck", map[string]string{"hashes": strings.Join(hashes, "|")})
}

// Reannounce torrents
func (client *Client) Reannounce(hashes []string) error {
	return client.getStatus("api/v2/torrents/reannounce", map[string]string{"hashes": strings.Join(hashes, "|")})

}

// DownloadLinks starts downloading a torrent from a link
func (client *Client) DownloadLinks(links []string, opts DownloadOptions) error {
	params := map[string]string{}
	if len(links) == 0 {
		return fmt.Errorf("at least one url must be present")
	} else {
		// TODO: Why is encoding causing problems now?
		// encodedURLS := url.QueryEscape(strings.JoinedURLs)
		params["urls"] = strings.Join(links, "%0A")
	}
	if opts.SavePath != nil {
		params["savepath"] = *opts.SavePath
	}
	if opts.Cookie != nil {
		params["cookie"] = *opts.Cookie
	}
	if opts.Category != nil {
		params["category"] = *opts.Category
	}
	if opts.SkipHashChecking != nil {
		params["skip_checking"] = strconv.FormatBool(*opts.SkipHashChecking)
	}
	if opts.Paused != nil {
		params["paused"] = strconv.FormatBool(*opts.Paused)
	}
	if opts.RootFolder != nil {
		params["root_folder"] = strconv.FormatBool(*opts.RootFolder)
	}
	if opts.Rename != nil {
		params["rename"] = *opts.Rename
	}
	if opts.UploadSpeedLimit != nil {
		params["upLimit"] = strconv.Itoa(*opts.UploadSpeedLimit)
	}
	if opts.DownloadSpeedLimit != nil {
		params["dlLimit"] = strconv.Itoa(*opts.DownloadSpeedLimit)
	}
	if opts.SequentialDownload != nil {
		params["sequentialDownload"] = strconv.FormatBool(*opts.SequentialDownload)
	}
	if opts.FirstLastPiecePriority != nil {
		params["firstLastPiecePrio"] = strconv.FormatBool(*opts.FirstLastPiecePriority)
	}

	resp, err := client.postMultipartData("api/v2/torrents/add", params)
	if err != nil {
		return err
	} else {
		_ = resp.Body.Close()
		if resp.StatusCode == 415 {
			return fmt.Errorf("torrent file is not valid")
		}
	}

	return nil
}

// DownloadFromFile starts downloading a torrent from a file
func (client *Client) DownloadFromFile(torrentPath string, opts DownloadOptions) error {
	if torrentPath == "" {
		return fmt.Errorf("torrentPath must be present")
	}
	var file *os.File
	file, err := os.Open(torrentPath)
	if err != nil {
		return err
	}
	err = client.DownloadFromReader(file, opts)
	_ = file.Close()
	return err
}

// DownloadFromReader starts downloading a torrent from io.Reader
func (client *Client) DownloadFromReader(torrent io.Reader, opts DownloadOptions) error {
	params := map[string]string{}
	if torrent == nil {
		return fmt.Errorf("reader must be present")
	}
	if opts.SavePath != nil {
		params["savepath"] = *opts.SavePath
	}
	if opts.Cookie != nil {
		params["cookie"] = *opts.Cookie
	}
	if opts.Category != nil {
		params["category"] = *opts.Category
	}
	if opts.SkipHashChecking != nil {
		params["skip_checking"] = strconv.FormatBool(*opts.SkipHashChecking)
	}
	if opts.Paused != nil {
		params["paused"] = strconv.FormatBool(*opts.Paused)
	}
	if opts.RootFolder != nil {
		params["root_folder"] = strconv.FormatBool(*opts.RootFolder)
	}
	if opts.Rename != nil {
		params["rename"] = *opts.Rename
	}
	if opts.UploadSpeedLimit != nil {
		params["upLimit"] = strconv.Itoa(*opts.UploadSpeedLimit)
	}
	if opts.DownloadSpeedLimit != nil {
		params["dlLimit"] = strconv.Itoa(*opts.DownloadSpeedLimit)
	}
	if opts.AutomaticTorrentManagement != nil {
		params["autoTMM"] = strconv.FormatBool(*opts.AutomaticTorrentManagement)
	}
	if opts.SequentialDownload != nil {
		params["sequentialDownload"] = strconv.FormatBool(*opts.SequentialDownload)
	}
	if opts.FirstLastPiecePriority != nil {
		params["firstLastPiecePrio"] = strconv.FormatBool(*opts.FirstLastPiecePriority)
	}
	resp, err := client.postMultipartReader("api/v2/torrents/add", torrent, params)
	if err != nil {
		return err
	} else {
		_ = resp.Body.Close()
		if resp.StatusCode == 415 {
			return fmt.Errorf("torrent file is not valid")
		}
	}

	return nil
}

// AddTrackers to a torrent
func (client *Client) AddTrackers(hash string, trackers []string) error {
	resp, err := client.post("api/v2/torrents/addTrackers",
		map[string]string{"hash": strings.ToLower(hash), "urls": url.QueryEscape(strings.Join(trackers, "%0A"))})
	if err != nil {
		return err
	} else {
		_ = resp.Body.Close()
		if resp.StatusCode == 404 {
			return fmt.Errorf("torrent hash not found")
		}
	}
	return nil
}

// EditTracker on a torrent
func (client *Client) EditTracker(hash string, origURL string, newURL string) error {
	params := map[string]string{
		"hash":    hash,
		"origUrl": origURL,
		"newUrl":  newURL,
	}
	resp, err := client.get("api/v2/torrents/editTracker", params)
	if err != nil {
		return err
	}
	_ = resp.Body.Close()
	switch resp.StatusCode {
	case 400:
		return fmt.Errorf("newUrl is not a valid url")
	case 404:
		return fmt.Errorf("torrent hash was not found")
	case 409:
		return fmt.Errorf("newUrl already exists for this torrent or origUrl was not found")
	default:
		return nil
	}
}

// RemoveTrackers from a torrent
func (client *Client) RemoveTrackers(hash string, trackers []string) error {
	params := map[string]string{
		"hash": hash,
		"urls": strings.Join(trackers, "|"),
	}
	resp, err := client.get("api/v2/torrents/removeTrackers", params)
	if err != nil {
		return err
	}

	_ = resp.Body.Close()
	switch sc := resp.StatusCode; sc {
	case 200:
		return nil
	case 404:
		return fmt.Errorf("torrent hash was not found")
	case 409:
		return fmt.Errorf("all URLs were not found")
	default:
		return fmt.Errorf("an unknown error occurred causing a status code of: %v", sc)
	}
}

// IncreasePriority of torrents
func (client *Client) IncreasePriority(hashes []string) error {
	opts := map[string]string{"hashes": strings.Join(hashes, "|")}
	resp, err := client.post("api/v2/torrents/increasePrio", opts)
	if err != nil {
		return err
	}

	_ = resp.Body.Close()
	switch sc := resp.StatusCode; sc {
	case 200:
		return nil
	case 409:
		return fmt.Errorf("torrent queueing is not enabled")
	default:
		return fmt.Errorf("an unknown error occurred causing a status code of: %v", sc)
	}
}

// DecreasePriority of torrents
func (client *Client) DecreasePriority(hashes []string) error {
	opts := map[string]string{"hashes": strings.Join(hashes, "|")}
	resp, err := client.post("api/v2/torrents/decreasePrio", opts)
	if err != nil {
		return err
	}

	_ = resp.Body.Close()
	switch sc := resp.StatusCode; sc {
	case 200:
		return nil
	case 409:
		return fmt.Errorf("torrent queueing is not enabled")
	default:
		return fmt.Errorf("an unknown error occurred causing a status code of: %v", sc)
	}
}

// MaxPriority maximizes the priority of torrents
func (client *Client) MaxPriority(hashes []string) error {
	opts := map[string]string{"hashes": strings.Join(hashes, "|")}
	resp, err := client.post("api/v2/torrents/topPrio", opts)
	if err != nil {
		return err
	}

	_ = resp.Body.Close()
	switch sc := resp.StatusCode; sc {
	case 200:
		return nil
	case 409:
		return fmt.Errorf("torrent queueing is not enabled")
	default:
		return fmt.Errorf("an unknown error occurred causing a status code of: %v", sc)
	}
}

// MinPriority maximizes the priority of torrents
func (client *Client) MinPriority(hashes []string) error {
	opts := map[string]string{"hashes": strings.Join(hashes, "|")}
	resp, err := client.post("api/v2/torrents/bottomPrio", opts)
	if err != nil {
		return err
	}

	_ = resp.Body.Close()
	switch sc := resp.StatusCode; sc {
	case 200:
		return nil
	case 409:
		return fmt.Errorf("torrent queueing is not enabled")
	default:
		return fmt.Errorf("an unknown error occurred causing a status code of: %v", sc)
	}
}

// FilePriority for a torrent
func (client *Client) FilePriority(hash string, ids []int, priority int) error {
	formattedIds := make([]string, len(ids))
	for i, id := range ids {
		formattedIds[i] = strconv.Itoa(id)
	}

	opts := map[string]string{
		"hash":     hash,
		"id":       strings.Join(formattedIds, "|"),
		"priority": strconv.Itoa(priority),
	}
	resp, err := client.post("api/v2/torrents/filePrio", opts)
	if err != nil {
		return err
	}

	_ = resp.Body.Close()
	switch sc := resp.StatusCode; sc {
	case 200:
		return nil
	case 400:
		return fmt.Errorf("priority is invalid or at least one id is not an integer")
	case 409:
		return fmt.Errorf("torrent metadata hasn't downloaded yet or at least one file id was not found")
	default:
		return fmt.Errorf("an unknown error occurred causing a status code of: %v", sc)
	}
}

// GetTorrentDownloadLimit for a list of torrents
func (client *Client) GetTorrentDownloadLimit(hashes []string) (limits map[string]int, err error) {
	err = client.postJSON("api/v2/torrents/downloadLimit", map[string]string{"hashes": strings.Join(hashes, "|")},
		&limits)
	return
}

// SetTorrentDownloadLimit for a list of torrents
func (client *Client) SetTorrentDownloadLimit(hashes []string, limit int) (bool, error) {
	opts := map[string]string{
		"hashes": strings.Join(hashes, "|"),
		"limit":  strconv.Itoa(limit),
	}
	resp, err := client.post("api/v2/torrents/setDownloadLimit", opts)
	if err != nil {
		return false, err
	}
	_ = resp.Body.Close()
	return resp.StatusCode == 200, nil
}

// SetTorrentShareLimit for a list of torrents
func (client *Client) SetTorrentShareLimit(hashes []string, ratioLimit int, seedingTimeLimit int) (bool, error) {
	opts := map[string]string{
		"hashes":           strings.Join(hashes, "|"),
		"ratioLimit":       strconv.Itoa(ratioLimit),
		"seedingTimeLimit": strconv.Itoa(seedingTimeLimit),
	}
	resp, err := client.post("api/v2/torrents/setShareLimits", opts)
	if err != nil {
		return false, err
	}
	_ = resp.Body.Close()
	return resp.StatusCode == 200, nil
}

// GetTorrentUploadLimit for a list of torrents
func (client *Client) GetTorrentUploadLimit(hashes []string) (limits map[string]int, err error) {
	err = client.postJSON("api/v2/torrents/uploadLimit", map[string]string{"hashes": strings.Join(hashes, "|")},
		&limits)
	return
}

// SetTorrentUploadLimit for a list of torrents
func (client *Client) SetTorrentUploadLimit(hashes []string, limit int) (bool, error) {
	opts := map[string]string{
		"hashes": strings.Join(hashes, "|"),
		"limit":  strconv.Itoa(limit),
	}
	resp, err := client.post("api/v2/torrents/setUploadLimit", opts)
	if err != nil {
		return false, err
	}
	_ = resp.Body.Close()

	return resp.StatusCode == 200, nil
}

// SetTorrentLocation for a list of torrents
func (client *Client) SetTorrentLocation(hashes []string, location string) (bool, error) {
	opts := map[string]string{
		"hashes":   strings.Join(hashes, "|"),
		"location": location,
	}
	resp, err := client.post("api/v2/torrents/setLocation", opts)
	if err != nil {
		return false, err
	}
	_ = resp.Body.Close()

	return resp.StatusCode == 200, nil //TODO: look into other statuses
}

// SetTorrentName for a torrent
func (client *Client) SetTorrentName(hash string, name string) (bool, error) {
	opts := map[string]string{
		"hash": hash,
		"name": name,
	}
	resp, err := client.post("api/v2/torrents/rename", opts)
	if err != nil {
		return false, err
	}
	_ = resp.Body.Close()

	return resp.StatusCode == 200, nil //TODO: look into other statuses
}

// SetTorrentCategory for a list of torrents
func (client *Client) SetTorrentCategory(hashes []string, category string) (bool, error) {
	opts := map[string]string{
		"hashes":   strings.Join(hashes, "|"),
		"category": category,
	}
	resp, err := client.post("api/v2/torrents/setCategory", opts)
	if err != nil {
		return false, err
	}
	_ = resp.Body.Close()

	return resp.StatusCode == 200, nil //TODO: look into other statuses
}

// GetCategories used by client
func (client *Client) GetCategories() (categories Categories, err error) {
	err = client.getJSON("api/v2/torrents/categories", nil, &categories)
	return
}

// CreateCategory for use by client
func (client *Client) CreateCategory(category string, savePath string) (bool, error) {
	opts := map[string]string{
		"category": category,
		"savePath": savePath,
	}
	resp, err := client.post("api/v2/torrents/createCategory", opts)
	if err != nil {
		return false, err
	}
	_ = resp.Body.Close()

	return resp.StatusCode == 200, nil //TODO: look into other statuses
}

// UpdateCategory used by client
func (client *Client) UpdateCategory(category string, savePath string) (bool, error) {
	opts := map[string]string{
		"category": category,
		"savePath": savePath,
	}
	resp, err := client.post("api/v2/torrents/editCategory", opts)
	if err != nil {
		return false, err
	}
	_ = resp.Body.Close()

	return resp.StatusCode == 200, nil //TODO: look into other statuses
}

// DeleteCategories used by client
func (client *Client) DeleteCategories(categories []string) (bool, error) {
	opts := map[string]string{"categories": strings.Join(categories, "\n")}
	resp, err := client.post("api/v2/torrents/removeCategories", opts)
	if err != nil {
		return false, err
	}
	_ = resp.Body.Close()

	return resp.StatusCode == 200, nil //TODO: look into other statuses
}

// AddTorrentTags to a list of torrents
func (client *Client) AddTorrentTags(hashes []string, tags []string) (bool, error) {
	opts := map[string]string{
		"hashes": strings.Join(hashes, "|"),
		"tags":   strings.Join(tags, ","),
	}
	resp, err := client.post("api/v2/torrents/addTags", opts)
	if err != nil {
		return false, err
	}
	_ = resp.Body.Close()

	return resp.StatusCode == 200, nil //TODO: look into other statuses
}

// RemoveTorrentTags from a list of torrents (empty list removes all tags)
func (client *Client) RemoveTorrentTags(hashes []string, tags []string) (bool, error) {
	opts := map[string]string{
		"hashes": strings.Join(hashes, "|"),
		"tags":   strings.Join(tags, ","),
	}
	resp, err := client.post("api/v2/torrents/removeTags", opts)
	if err != nil {
		return false, err
	}
	_ = resp.Body.Close()

	return resp.StatusCode == 200, nil //TODO: look into other statuses
}

// GetTorrentTags from a list of torrents (empty list removes all tags)
func (client *Client) GetTorrentTags() (tags []string, err error) {
	err = client.getJSON("api/v2/torrents/tags", nil, &tags)
	return
}

// CreateTags for use by client
func (client *Client) CreateTags(tags []string) (bool, error) {
	opts := map[string]string{"tags": strings.Join(tags, ",")}
	resp, err := client.post("api/v2/torrents/createTags", opts)
	if err != nil {
		return false, err
	}
	_ = resp.Body.Close()

	return resp.StatusCode == 200, nil //TODO: look into other statuses
}

// DeleteTags used by client
func (client *Client) DeleteTags(tags []string) (bool, error) {
	opts := map[string]string{"tags": strings.Join(tags, ",")}
	resp, err := client.post("api/v2/torrents/deleteTags", opts)
	if err != nil {
		return false, err
	}
	_ = resp.Body.Close()

	return resp.StatusCode == 200, nil //TODO: look into other statuses
}

// SetAutoManagement for a list of torrents
func (client *Client) SetAutoManagement(hashes []string, enable bool) (bool, error) {
	opts := map[string]string{
		"hashes": strings.Join(hashes, "|"),
		"enable": strconv.FormatBool(enable),
	}
	resp, err := client.post("api/v2/torrents/setAutoManagement", opts)
	if err != nil {
		return false, err
	}
	_ = resp.Body.Close()

	return resp.StatusCode == 200, nil //TODO: look into other statuses
}

// ToggleSequentialDownload for a list of torrents
func (client *Client) ToggleSequentialDownload(hashes []string) (bool, error) {
	opts := map[string]string{"hashes": strings.Join(hashes, "|")}
	resp, err := client.get("api/v2/torrents/toggleSequentialDownload", opts)
	if err != nil {
		return false, err
	}
	_ = resp.Body.Close()

	return resp.StatusCode == 200, nil //TODO: look into other statuses
}

// ToggleFirstLastPiecePriority for a list of torrents
func (client *Client) ToggleFirstLastPiecePriority(hashes []string) error {
	return client.getStatus("api/v2/torrents/toggleFirstLastPiecePrio",
		map[string]string{"hashes": strings.Join(hashes, "|")})
}

// SetForceStart for a list of torrents
func (client *Client) SetForceStart(hashes []string, value bool) (bool, error) {
	opts := map[string]string{
		"hashes": strings.Join(hashes, "|"),
		"value":  strconv.FormatBool(value),
	}
	resp, err := client.post("api/v2/torrents/setForceStart", opts)
	if err != nil {
		return false, err
	}
	_ = resp.Body.Close()

	return resp.StatusCode == 200, nil //TODO: look into other statuses
}

// SetSuperSeeding for a list of torrents
func (client *Client) SetSuperSeeding(hashes []string, value bool) (bool, error) {
	opts := map[string]string{
		"hashes": strings.Join(hashes, "|"),
		"value":  strconv.FormatBool(value),
	}
	resp, err := client.post("api/v2/torrents/setSuperSeeding", opts)
	if err != nil {
		return false, err
	}
	_ = resp.Body.Close()

	return resp.StatusCode == 200, nil //TODO: look into other statuses
}
