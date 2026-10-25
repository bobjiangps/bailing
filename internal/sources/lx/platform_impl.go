package lx

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// nowMillis is replaceable in tests.
var nowMillis = func() int64 { return time.Now().UnixMilli() }

// ---- kw (Kuwo) ---------------------------------------------------------------------

type kwPlatform struct{}

func (kwPlatform) search(ctx context.Context, c *http.Client, q string, page, limit int) ([]map[string]any, int, error) {
	v := url.Values{}
	v.Set("client", "kt")
	v.Set("all", q)
	v.Set("pn", strconv.Itoa(page-1))
	v.Set("rn", strconv.Itoa(limit))
	v.Set("uid", "794762570")
	v.Set("ver", "kwplayer_ar_9.2.2.1")
	v.Set("vipver", "1")
	v.Set("show_copyright_off", "1")
	v.Set("newver", "1")
	v.Set("ft", "music")
	v.Set("cluster", "0")
	v.Set("strategy", "2012")
	v.Set("encoding", "utf8")
	v.Set("rformat", "json")
	v.Set("vermerge", "1")
	v.Set("mobi", "1")
	v.Set("issubtitle", "1")
	req, _ := http.NewRequest(http.MethodGet, "http://search.kuwo.cn/r.s?"+v.Encode(), nil)
	var res struct {
		Total   flex `json:"TOTAL"`
		AbsList []struct {
			RID      string `json:"MUSICRID"`
			Name     string `json:"SONGNAME"`
			Artist   string `json:"ARTIST"`
			Album    string `json:"ALBUM"`
			AlbumID  flex   `json:"ALBUMID"`
			Duration flex   `json:"DURATION"`
			MInfo    string `json:"N_MINFO"`
			Pic      string `json:"web_albumpic_short"`
		} `json:"abslist"`
	}
	if err := getJSON(ctx, c, req, &res); err != nil {
		return nil, 0, err
	}
	out := make([]map[string]any, 0, len(res.AbsList))
	for _, it := range res.AbsList {
		id := strings.TrimPrefix(it.RID, "MUSIC_")
		if id == "" {
			continue
		}
		img := ""
		if it.Pic != "" {
			img = "https://img2.kuwo.cn/star/albumcover/500" + strings.TrimPrefix(it.Pic, "120")
		}
		sec := it.Duration.Int()
		mi := baseInfo("kw", id, clean(it.Name), clean(strings.ReplaceAll(it.Artist, "&", "、")),
			clean(it.Album), string(it.AlbumID), sec, img)
		var items [][3]string
		for _, part := range strings.Split(it.MInfo, ";") {
			kv := map[string]string{}
			for _, f := range strings.Split(part, ",") {
				if k, val, ok := strings.Cut(f, ":"); ok {
					kv[k] = val
				}
			}
			switch kv["bitrate"] {
			case "4000":
				items = append(items, [3]string{"flac24bit", strings.ToUpper(kv["size"]), ""})
			case "2000":
				items = append(items, [3]string{"flac", strings.ToUpper(kv["size"]), ""})
			case "320":
				items = append(items, [3]string{"320k", strings.ToUpper(kv["size"]), ""})
			case "128":
				items = append(items, [3]string{"128k", strings.ToUpper(kv["size"]), ""})
			}
		}
		mi["types"], mi["_types"] = typesOf(items...)
		mi["_sec"] = sec
		out = append(out, mi)
	}
	return out, int(res.Total.Int()), nil
}

// ---- kg (Kugou) --------------------------------------------------------------------

type kgPlatform struct{}

func (kgPlatform) search(ctx context.Context, c *http.Client, q string, page, limit int) ([]map[string]any, int, error) {
	v := url.Values{}
	v.Set("keyword", q)
	v.Set("page", strconv.Itoa(page))
	v.Set("pagesize", strconv.Itoa(limit))
	v.Set("userid", "0")
	v.Set("clientver", "")
	v.Set("platform", "WebFilter")
	v.Set("filter", "2")
	v.Set("iscorrection", "1")
	v.Set("privilege_filter", "0")
	v.Set("area_code", "1")
	req, _ := http.NewRequest(http.MethodGet, "https://songsearch.kugou.com/song_search_v2?"+v.Encode(), nil)
	var res struct {
		Data struct {
			Total flex `json:"total"`
			Lists []struct {
				SongName   string `json:"SongName"`
				SingerName string `json:"SingerName"`
				Singers    []struct {
					Name string `json:"name"`
				} `json:"Singers"`
				AlbumName   string `json:"AlbumName"`
				AlbumID     flex   `json:"AlbumID"`
				AudioID     flex   `json:"Audioid"`
				FileHash    string `json:"FileHash"`
				HQFileHash  string `json:"HQFileHash"`
				SQFileHash  string `json:"SQFileHash"`
				ResFileHash string `json:"ResFileHash"`
				FileSize    flex   `json:"FileSize"`
				HQFileSize  flex   `json:"HQFileSize"`
				SQFileSize  flex   `json:"SQFileSize"`
				ResFileSize flex   `json:"ResFileSize"`
				Duration    flex   `json:"Duration"`
				Image       string `json:"Image"`
			} `json:"lists"`
		} `json:"data"`
	}
	if err := getJSON(ctx, c, req, &res); err != nil {
		return nil, 0, err
	}
	out := make([]map[string]any, 0, len(res.Data.Lists))
	for _, it := range res.Data.Lists {
		var names []string
		for _, s := range it.Singers {
			names = append(names, s.Name)
		}
		singer := strings.Join(names, "、")
		if singer == "" {
			singer = it.SingerName
		}
		img := strings.ReplaceAll(it.Image, "{size}", "480")
		sec := it.Duration.Int()
		mi := baseInfo("kg", string(it.AudioID), clean(it.SongName), clean(singer), clean(it.AlbumName),
			string(it.AlbumID), sec, img)
		mi["hash"] = it.FileHash
		mi["types"], mi["_types"] = typesOf(
			[3]string{"128k", sizeStr(it.FileSize.Int()), it.FileHash},
			[3]string{"320k", sizeStr(it.HQFileSize.Int()), it.HQFileHash},
			[3]string{"flac", sizeStr(it.SQFileSize.Int()), it.SQFileHash},
			[3]string{"flac24bit", sizeStr(it.ResFileSize.Int()), it.ResFileHash},
		)
		mi["_sec"] = sec
		out = append(out, mi)
	}
	return out, int(res.Data.Total.Int()), nil
}

// ---- tx (QQ Music) -----------------------------------------------------------------

type txPlatform struct{}

func (txPlatform) search(ctx context.Context, c *http.Client, q string, page, limit int) ([]map[string]any, int, error) {
	body := map[string]any{
		"comm": map[string]any{"ct": "19", "cv": "1859", "uin": "0"},
		"req": map[string]any{
			"module": "music.search.SearchCgiService",
			"method": "DoSearchForQQMusicDesktop",
			"param": map[string]any{"grp": 1, "num_per_page": limit, "page_num": page,
				"query": q, "search_type": 0},
		},
	}
	b, _ := json.Marshal(body)
	req, _ := http.NewRequest(http.MethodPost, "https://u.y.qq.com/cgi-bin/musicu.fcg", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Referer", "https://y.qq.com/")
	var res struct {
		Req struct {
			Data struct {
				Meta struct {
					Sum flex `json:"sum"`
				} `json:"meta"`
				Body struct {
					Song struct {
						List []struct {
							ID       flex   `json:"id"`
							Mid      string `json:"mid"`
							Name     string `json:"name"`
							Title    string `json:"title"`
							Interval flex   `json:"interval"`
							Singer   []struct {
								Mid  string `json:"mid"`
								Name string `json:"name"`
							} `json:"singer"`
							Album struct {
								Mid  string `json:"mid"`
								Name string `json:"name"`
							} `json:"album"`
							File struct {
								MediaMid string `json:"media_mid"`
								Size128  flex   `json:"size_128mp3"`
								Size320  flex   `json:"size_320mp3"`
								SizeFlac flex   `json:"size_flac"`
								SizeHi   flex   `json:"size_hires"`
							} `json:"file"`
						} `json:"list"`
					} `json:"song"`
				} `json:"body"`
			} `json:"data"`
		} `json:"req"`
	}
	if err := getJSON(ctx, c, req, &res); err != nil {
		return nil, 0, err
	}
	list := res.Req.Data.Body.Song.List
	out := make([]map[string]any, 0, len(list))
	for _, it := range list {
		var names []string
		for _, s := range it.Singer {
			names = append(names, s.Name)
		}
		img := ""
		if it.Album.Mid != "" {
			img = "https://y.gtimg.cn/music/photo_new/T002R500x500M000" + it.Album.Mid + ".jpg"
		} else if len(it.Singer) > 0 && it.Singer[0].Mid != "" {
			img = "https://y.gtimg.cn/music/photo_new/T001R500x500M000" + it.Singer[0].Mid + ".jpg"
		}
		name := it.Name
		if name == "" {
			name = it.Title
		}
		sec := it.Interval.Int()
		mi := baseInfo("tx", it.Mid, clean(name), clean(strings.Join(names, "、")), clean(it.Album.Name),
			it.Album.Mid, sec, img)
		mi["songId"] = it.ID.Int()
		mi["albumMid"] = it.Album.Mid
		mi["strMediaMid"] = it.File.MediaMid
		mi["types"], mi["_types"] = typesOf(
			[3]string{"128k", sizeStr(it.File.Size128.Int()), ""},
			[3]string{"320k", sizeStr(it.File.Size320.Int()), ""},
			[3]string{"flac", sizeStr(it.File.SizeFlac.Int()), ""},
			[3]string{"flac24bit", sizeStr(it.File.SizeHi.Int()), ""},
		)
		mi["_sec"] = sec
		out = append(out, mi)
	}
	return out, int(res.Req.Data.Meta.Sum.Int()), nil
}

// ---- wy (NetEase Cloud Music) ------------------------------------------------------

type wyPlatform struct{}

const wyEapiKey = "e82ckenh8dichen8"

// eapiParams builds the encrypted "params" form field of NetEase's eapi.
func eapiParams(path string, data any) (string, error) {
	b, err := json.Marshal(data)
	if err != nil {
		return "", err
	}
	text := string(b)
	digest := md5Hex([]byte("nobody" + path + "use" + text + "md5forencrypt"))
	msg := path + "-36cd479b6b5-" + text + "-36cd479b6b5-" + digest
	enc, err := aesCrypt("aes-128-ecb", []byte(msg), []byte(wyEapiKey), nil, false)
	if err != nil {
		return "", err
	}
	return strings.ToUpper(hex.EncodeToString(enc)), nil
}

func (wyPlatform) search(ctx context.Context, c *http.Client, q string, page, limit int) ([]map[string]any, int, error) {
	params, err := eapiParams("/api/cloudsearch/pc", map[string]any{
		"s": q, "type": 1, "limit": limit, "total": page == 1, "offset": limit * (page - 1),
	})
	if err != nil {
		return nil, 0, err
	}
	form := url.Values{"params": {params}}
	req, _ := http.NewRequest(http.MethodPost, "https://interface.music.163.com/eapi/batch",
		strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Origin", "https://music.163.com")
	type size struct {
		Size flex `json:"size"`
	}
	var res struct {
		Code   int `json:"code"`
		Result struct {
			SongCount flex `json:"songCount"`
			Songs     []struct {
				ID   flex   `json:"id"`
				Name string `json:"name"`
				Ar   []struct {
					Name string `json:"name"`
				} `json:"ar"`
				Al struct {
					ID     flex   `json:"id"`
					Name   string `json:"name"`
					PicURL string `json:"picUrl"`
				} `json:"al"`
				Dt flex  `json:"dt"`
				L  *size `json:"l"`
				H  *size `json:"h"`
				Sq *size `json:"sq"`
				Hr *size `json:"hr"`
			} `json:"songs"`
		} `json:"result"`
	}
	if err := getJSON(ctx, c, req, &res); err != nil {
		return nil, 0, err
	}
	if res.Code != 200 && res.Code != 0 {
		return nil, 0, fmt.Errorf("code %d", res.Code)
	}
	sz := func(s *size) string {
		if s == nil {
			return ""
		}
		return sizeStr(s.Size.Int())
	}
	out := make([]map[string]any, 0, len(res.Result.Songs))
	for _, it := range res.Result.Songs {
		var names []string
		for _, a := range it.Ar {
			names = append(names, a.Name)
		}
		sec := it.Dt.Int() / 1000
		mi := baseInfo("wy", string(it.ID), clean(it.Name), clean(strings.Join(names, "、")),
			clean(it.Al.Name), string(it.Al.ID), sec, it.Al.PicURL)
		mi["types"], mi["_types"] = typesOf(
			[3]string{"128k", sz(it.L), ""},
			[3]string{"320k", sz(it.H), ""},
			[3]string{"flac", sz(it.Sq), ""},
			[3]string{"flac24bit", sz(it.Hr), ""},
		)
		mi["_sec"] = sec
		out = append(out, mi)
	}
	return out, int(res.Result.SongCount.Int()), nil
}

// ---- mg (Migu) ---------------------------------------------------------------------

type mgPlatform struct{}

// Fixed device id and salts used by the Migu Android app (as in LX Music).
const (
	mgDeviceID = "963B7AA0D21511ED807EE5846EC87D20"
	mgSignSalt = "6cdc72a439cef99a3418d2a78aa28c73yyapp2d16148780a1dcc7408e06336b98cfd50"
	mgUA       = "Mozilla/5.0 (Linux; U; Android 11.0.0; zh-cn; MI 11 Build/OPR1.170623.032) " +
		"AppleWebKit/534.30 (KHTML, like Gecko) Version/4.0 Mobile Safari/534.30"
	mgSwitch = `{"song":1,"album":0,"singer":0,"tagSong":1,"mvSong":0,"bestShow":1,"songlist":0,"lyricSong":0}`
)

// mgSign signs a search request: md5(keyword + salt + deviceId + timestamp).
func mgSign(q, ts string) string {
	return md5Hex([]byte(q + mgSignSalt + mgDeviceID + ts))
}

func mgImg(s string) string {
	switch {
	case s == "":
		return ""
	case strings.HasPrefix(s, "http"):
		return s
	default:
		return "https://d.musicapp.migu.cn" + s
	}
}

func (mgPlatform) search(ctx context.Context, c *http.Client, q string, page, limit int) ([]map[string]any, int, error) {
	ts := strconv.FormatInt(nowMillis(), 10)
	v := url.Values{}
	v.Set("isCorrect", "0")
	v.Set("isCopyright", "1")
	v.Set("searchSwitch", mgSwitch)
	v.Set("pageSize", strconv.Itoa(limit))
	v.Set("text", q)
	v.Set("pageNo", strconv.Itoa(page))
	v.Set("sort", "0")
	v.Set("sid", "USS")
	req, _ := http.NewRequest(http.MethodGet,
		"https://jadeite.migu.cn/music_search/v3/search/searchAll?"+v.Encode(), nil)
	req.Header.Set("uiVersion", "A_music_3.6.1")
	req.Header.Set("deviceId", mgDeviceID)
	req.Header.Set("timestamp", ts)
	req.Header.Set("sign", mgSign(q, ts))
	req.Header.Set("channel", "0146921")
	req.Header.Set("User-Agent", mgUA)
	type format struct {
		Type  string `json:"formatType"`
		ASize flex   `json:"asize"`
		ISize flex   `json:"isize"`
	}
	type song struct {
		SongID      flex   `json:"songId"`
		CopyrightID flex   `json:"copyrightId"`
		Name        string `json:"name"`
		Album       string `json:"album"`
		AlbumID     flex   `json:"albumId"`
		Duration    flex   `json:"duration"`
		Img1        string `json:"img1"`
		Img2        string `json:"img2"`
		Img3        string `json:"img3"`
		LrcURL      string `json:"lrcUrl"`
		MrcURL      string `json:"mrcurl"`
		TrcURL      string `json:"trcUrl"`
		Singers     []struct {
			Name string `json:"name"`
		} `json:"singerList"`
		Formats []format `json:"audioFormats"`
	}
	var res struct {
		Code string `json:"code"`
		Info string `json:"info"`
		Data struct {
			Total      flex     `json:"totalCount"`
			ResultList [][]song `json:"resultList"`
		} `json:"songResultData"`
	}
	if err := getJSON(ctx, c, req, &res); err != nil {
		return nil, 0, err
	}
	if res.Code != "000000" {
		return nil, 0, fmt.Errorf("code %s %s", res.Code, res.Info)
	}
	seen := map[string]bool{}
	var out []map[string]any
	for _, group := range res.Data.ResultList {
		for _, it := range group {
			id := string(it.SongID)
			if id == "" || seen[id] {
				continue
			}
			seen[id] = true
			var names []string
			for _, s := range it.Singers {
				names = append(names, s.Name)
			}
			sizes := map[string]string{}
			for _, f := range it.Formats {
				n := f.ASize.Int()
				if n == 0 {
					n = f.ISize.Int()
				}
				sizes[f.Type] = sizeStr(n)
			}
			sec := it.Duration.Int()
			img := mgImg(it.Img3)
			if img == "" {
				img = mgImg(it.Img2)
			}
			if img == "" {
				img = mgImg(it.Img1)
			}
			mi := baseInfo("mg", id, clean(it.Name), clean(strings.Join(names, "、")),
				clean(it.Album), string(it.AlbumID), sec, img)
			mi["copyrightId"] = string(it.CopyrightID)
			mi["lrcUrl"] = it.LrcURL
			mi["mrcUrl"] = it.MrcURL
			mi["trcUrl"] = it.TrcURL
			mi["types"], mi["_types"] = typesOf(
				[3]string{"128k", sizes["PQ"], ""},
				[3]string{"320k", sizes["HQ"], ""},
				[3]string{"flac", sizes["SQ"], ""},
				[3]string{"flac24bit", sizes["ZQ24"], ""},
			)
			mi["_sec"] = sec
			out = append(out, mi)
		}
	}
	return out, int(res.Data.Total.Int()), nil
}
