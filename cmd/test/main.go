package main

import (
	"flag"
	"fmt"

	"github.com/sot-tech/go-qbittorrent/client"
)

func main() {
	var url, login, password, downLink, searchHash string
	flag.StringVar(&url, "url", "http://localhost:8181", "url to qBittorrent server")
	flag.StringVar(&login, "login", "", "login for qBittorrent server")
	flag.StringVar(&password, "password", "", "password for qBittorrent server")
	flag.StringVar(&downLink, "downLink", "", "torrent URL to download")
	flag.StringVar(&searchHash, "searchHash", "", "torrent hash to search")
	flag.Parse()
	// connect to qbittorrent client
	qb := client.NewClient(url, 0)

	if len(login) > 0 {
		// login to the client
		loginOpts := client.LoginOptions{
			Username: login,
			Password: password,
		}
		err := qb.Login(loginOpts)
		if err != nil {
			fmt.Println(err)
			return
		}
	}

	info, err := qb.Info()
	if err != nil {
		fmt.Println("[-] Info error:", err)
	} else {
		fmt.Println("[+] Info:", info)
	}

	// ********************
	// DOWNLOAD A TORRENT *
	// ********************

	if len(downLink) > 0 {
		err := qb.DownloadLinks([]string{downLink}, client.DownloadOptions{})

		if err != nil {
			fmt.Println("[-] Download torrent from link error:", err)
		} else {
			fmt.Println("[+] Download torrent from link ok")
		}
	} else {
		fmt.Println("[?] Download link not set")
	}

	// ******************
	// GET ALL TORRENTS *
	// ******************
	sort := "name"
	torrentsOpts := client.TorrentsOptions{Sort: &sort}
	if len(searchHash) > 0 {
		torrentsOpts.Hashes = []string{searchHash}
	}
	torrents, err := qb.Torrents(torrentsOpts)
	if err != nil {
		fmt.Println("[-] Get torrent list error:", err)
	} else {
		fmt.Println("[+] Get torrent list:", torrents)
	}
	if len(searchHash) > 0 {
		torrent, err := qb.Torrent(searchHash)
		if err != nil {
			fmt.Println("[-] Get torrent error:", err)
		} else {
			fmt.Println("[+] Get torrent:", torrent)
		}
	}
}
