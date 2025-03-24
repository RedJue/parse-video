package parser

import (
	"bytes"
	"errors"
	"fmt"
	"math/rand"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/go-resty/resty/v2"
	"github.com/tidwall/gjson"
)

// 预定义允许的抖音CDN域名列表
var allowedDouyinDomains = []string{
	"v93.douyinvod.com", "v5-che.douyinvod.com", "v6-qos-hourly.douyinvod.com", "v26-che.douyinvod.com",
	"v6-cold.douyinvod.com", "v83-x.douyinvod.com", "v5-coldb.douyinvod.com", "v3-z.douyinvod.com",
	"v1-x.douyinvod.com", "v6-ab-e1.douyinvod.com", "v5-abtest.douyinvod.com", "v9-che.douyinvod.com",
	"v83-y.douyinvod.com", "v5-litea.douyinvod.com", "v3-che.douyinvod.com", "v29-cold.douyinvod.com",
	"v5-lite.douyinvod.com", "v29-qos-control.douyinvod.com", "v5-gdgz.douyinvod.com", "v5-ttcp-a.douyinvod.com",
	"v3-b.douyinvod.com", "v9-z-qos-control.douyinvod.com", "v9-x-qos-hourly.douyinvod.com", "v9-chc.douyinvod.com",
	"v9-qos-hourly.douyinvod.com", "v5-ttcp-b.douyinvod.com", "v6-z-qos-control.douyinvod.com", "v5-dlyd.douyinvod.com",
	"v5-coldy.douyinvod.com", "v3-c.douyinvod.com", "v5-jbwl.douyinvod.com", "v26-0015c002.douyinvod.com",
	"v5-gdwy.douyinvod.com", "v3-d.douyinvod.com", "v3-p.douyinvod.com", "v5-gdhy.douyinvod.com",
	"v26-cold.douyinvod.com", "v5-lite-a.douyinvod.com", "v5-i.douyinvod.com", "v5-g.douyinvod.com",
	"v26-qos-daily.douyinvod.com", "v5-dash.douyinvod.com", "v5-h.douyinvod.com", "v5-f.douyinvod.com",
	"v3-a.douyinvod.com", "v83.douyinvod.com", "v5-cold.douyinvod.com", "v3-y.douyinvod.com",
	"v26-x.douyinvod.com", "v27-ipv6.douyinvod.com", "v9-ipv6.douyinvod.com", "v5-yacu.douyinvod.com",
	"v29-ipv6.douyinvod.com", "v26-coldf.douyinvod.com", "v5.douyinvod.com", "v11.douyinvod.com",
	"v6-z.douyinvod.com", "v1.douyinvod.com", "v9-y.douyinvod.com", "v9-z.douyinvod.com",
	"v9.douyinvod.com", "v3-x.douyinvod.com", "v6-y.douyinvod.com", "v3-ipv6.douyinvod.com",
	"v5-e.douyinvod.com", "v3.douyinvod.com", "v6-ipv6.douyinvod.com", "v9-x.douyinvod.com",
	"v6-p.douyinvod.com", "v1-2p.douyinvod.com", "v1-p.douyinvod.com", "v1-ipv6.douyinvod.com",
	"v24.douyinvod.com", "v1-dy.douyinvod.com", "v6.douyinvod.com", "v6-x.douyinvod.com",
	"v26-ipv6.douyinvod.com", "v27.douyinvod.com", "v92.douyinvod.com", "v95.douyinvod.com",
	"douyinvod.com", "v26.douyinvod.com", "v29.douyinvod.com",
}

type douYin struct{}

// isValidDouyinUrl 检查URL是否包含允许的抖音域名
func isValidDouyinUrl(videoUrl string) bool {
	if videoUrl == "" {
		return false
	}

	parsedUrl, err := url.Parse(videoUrl)
	if err != nil {
		return false
	}

	for _, domain := range allowedDouyinDomains {
		if strings.Contains(parsedUrl.Host, domain) {
			return true
		}
	}

	return false
}

func (d douYin) parseVideoID(videoId string) (*VideoParseInfo, error) {
	var videoInfo *VideoParseInfo
	var err error
	maxRetries := 1000

	// 尝试最多30次，直到获取到包含允许域名的视频链接
	for attempt := 1; attempt <= maxRetries; attempt++ {
		videoInfo, err = d.parseVideoIDOnce(videoId)
		if err != nil {
			// 如果解析出错，直接返回错误
			return nil, err
		}

		// 如果是图集或者没有视频URL，不需要验证域名
		if len(videoInfo.Images) > 0 || videoInfo.VideoUrl == "" {
			return videoInfo, nil
		}

		// 检查视频URL是否包含允许的域名
		if isValidDouyinUrl(videoInfo.VideoUrl) {
			return videoInfo, nil
		}

		// 如果不是最后一次尝试，则休眠一小段时间后重试
		if attempt < maxRetries {
			// 增加随机性，避免被限制
			time.Sleep(time.Duration(100+rand.Intn(200)) * time.Millisecond)
		}
	}

	// 如果所有尝试都失败了，返回最后一次获取的结果和提示
	if videoInfo != nil {
		return videoInfo, fmt.Errorf("video URL does not contain any allowed domains after %d attempts", maxRetries)
	}

	return nil, errors.New("failed to get valid video URL after 30 attempts")
}

// parseVideoIDOnce 是原来的parseVideoID函数的实现，只尝试解析一次
func (d douYin) parseVideoIDOnce(videoId string) (*VideoParseInfo, error) {
	reqUrl := fmt.Sprintf("https://www.iesdouyin.com/share/video/%s", videoId)

	client := resty.New()
	res, err := client.R().
		SetHeader(HttpHeaderUserAgent, "Mozilla/5.0 (iPhone; CPU iPhone OS 16_6 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/16.6 Mobile/15E148 Safari/604.1 Edg/122.0.0.0").
		Get(reqUrl)
	if err != nil {
		return nil, err
	}

	re := regexp.MustCompile(`window._ROUTER_DATA\s*=\s*(.*?)</script>`)
	findRes := re.FindSubmatch(res.Body())
	if len(findRes) < 2 {
		return nil, errors.New("parse video json info from html fail")
	}

	jsonBytes := bytes.TrimSpace(findRes[1])
	data := gjson.GetBytes(jsonBytes, "loaderData.video_(id)/page.videoInfoRes.item_list.0")

	if !data.Exists() {
		filterObj := gjson.GetBytes(
			jsonBytes,
			fmt.Sprintf(`loaderData.video_(id)/page.videoInfoRes.filter_list.#(aweme_id=="%s")`, videoId),
		)

		return nil, fmt.Errorf(
			"get video info fail: %s - %s",
			filterObj.Get("filter_reason"),
			filterObj.Get("detail_msg"),
		)
	}

	// 获取图集图片地址
	imagesObjArr := data.Get("images").Array()
	images := make([]string, 0, len(imagesObjArr))
	for _, imageItem := range imagesObjArr {
		imageUrl := imageItem.Get("url_list.0").String()
		if len(imageUrl) > 0 {
			images = append(images, imageUrl)
		}
	}

	// 获取视频播放地址
	videoUrl := data.Get("video.play_addr.url_list.0").String()
	videoUrl = strings.ReplaceAll(videoUrl, "playwm", "play")
	data.Get("video.play_addr.url_list").ForEach(func(key, value gjson.Result) bool {
		fmt.Println(strings.ReplaceAll(value.String(), "playwm", "play"))
		return true
	})

	// 如果图集地址不为空时，因为没有视频，上面抖音返回的视频地址无法访问，置空处理
	if len(images) > 0 {
		videoUrl = ""
	}

	videoInfo := &VideoParseInfo{
		Title:    data.Get("desc").String(),
		VideoUrl: videoUrl,
		MusicUrl: "",
		CoverUrl: data.Get("video.cover.url_list.0").String(),
		Images:   images,
	}
	videoInfo.Author.Uid = data.Get("author.sec_uid").String()
	videoInfo.Author.Name = data.Get("author.nickname").String()
	videoInfo.Author.Avatar = data.Get("author.avatar_thumb.url_list.0").String()

	// 视频地址非空时，获取302重定向之后的视频地址
	// 图集时，视频地址为空，不处理
	if len(videoInfo.VideoUrl) > 0 {
		d.getRedirectUrl(videoInfo)
	}

	return videoInfo, nil
}

func (d douYin) parseShareUrl(shareUrl string) (*VideoParseInfo, error) {
	urlRes, err := url.Parse(shareUrl)
	if err != nil {
		return nil, err
	}

	switch urlRes.Host {
	case "www.iesdouyin.com", "www.douyin.com":
		return d.parsePcShareUrl(shareUrl) // 解析电脑网页端链接
	case "v.douyin.com":
		return d.parseAppShareUrl(shareUrl) // 解析App分享链接
	}

	return nil, fmt.Errorf("douyin not support this host: %s", urlRes.Host)
}

func (d douYin) parseAppShareUrl(shareUrl string) (*VideoParseInfo, error) {
	// 适配App分享链接类型:
	// https://v.douyin.com/xxxxxx/

	client := resty.New()
	// disable redirects in the HTTP client, get params before redirects
	client.SetRedirectPolicy(resty.NoRedirectPolicy())
	res, err := client.R().
		SetHeader(HttpHeaderUserAgent, DefaultUserAgent).
		Get(shareUrl)
	// 非 resty.ErrAutoRedirectDisabled 错误时，返回错误
	if !errors.Is(err, resty.ErrAutoRedirectDisabled) {
		return nil, err
	}

	locationRes, err := res.RawResponse.Location()
	if err != nil {
		return nil, err
	}

	videoId, err := d.parseVideoIdFromPath(locationRes.Path)
	if err != nil {
		return nil, err
	}
	if len(videoId) <= 0 {
		return nil, errors.New("parse video id from share url fail")
	}

	// 西瓜视频解析方式不一样
	if strings.Contains(locationRes.Host, "ixigua.com") {
		return xiGua{}.parseVideoID(videoId)
	}

	return d.parseVideoID(videoId)
}

func (d douYin) parsePcShareUrl(shareUrl string) (*VideoParseInfo, error) {
	// 适配电脑网页端链接类型
	// https://www.iesdouyin.com/share/video/xxxxxx/
	// https://www.douyin.com/video/xxxxxx
	videoId, err := d.parseVideoIdFromPath(shareUrl)
	if err != nil {
		return nil, err
	}
	return d.parseVideoID(videoId)
}

func (d douYin) parseVideoIdFromPath(urlPath string) (string, error) {
	if len(urlPath) <= 0 {
		return "", errors.New("url path is empty")
	}

	urlPath = strings.Trim(urlPath, "/")
	urlSplit := strings.Split(urlPath, "/")

	// 获取最后一个元素
	if len(urlSplit) > 0 {
		return urlSplit[len(urlSplit)-1], nil
	}

	return "", errors.New("parse video id from path fail")
}

func (d douYin) getRedirectUrl(videoInfo *VideoParseInfo) {
	client := resty.New()
	client.SetRedirectPolicy(resty.NoRedirectPolicy())
	res2, _ := client.R().
		SetHeader(HttpHeaderUserAgent, DefaultUserAgent).
		Get(videoInfo.VideoUrl)
	locationRes, _ := res2.RawResponse.Location()
	if locationRes != nil {
		(*videoInfo).VideoUrl = locationRes.String()
	}
}

func (d douYin) randSeq(n int) string {
	letters := []rune("0123456789abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ")
	b := make([]rune, n)
	for i := range b {
		b[i] = letters[rand.Intn(len(letters))]
	}
	return string(b)
}
