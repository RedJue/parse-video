package parser

import (
	"errors"
	"fmt"
	"net/url"

	"strings"

	"github.com/go-resty/resty/v2"
	"github.com/tidwall/gjson"
)

type bilibili struct{}

func (b bilibili) parseShareUrl(shareUrl string) (*VideoParseInfo, error) {
	// 处理短链接重定向
	if strings.Contains(shareUrl, "b23.tv") {
		client := resty.New()
		client.SetRedirectPolicy(resty.NoRedirectPolicy())
		resp, err := client.R().
			SetHeader(HttpHeaderUserAgent, DefaultUserAgent).
			Get(shareUrl)
		
		if !errors.Is(err, resty.ErrAutoRedirectDisabled) {
			return nil, err
		}

		location, err := resp.RawResponse.Location()
		if err != nil {
			return nil, err
		}
		shareUrl = location.String()
	}

	// 解析视频ID
	urlObj, err := url.Parse(shareUrl)
	if err != nil {
		return nil, err
	}

	var bvid string
	pathParts := strings.Split(strings.Trim(urlObj.Path, "/"), "/")
	for i, part := range pathParts {
		if strings.HasPrefix(part, "BV") {
			bvid = part
			break
		}
		if part == "video" && i+1 < len(pathParts) {
			bvid = pathParts[i+1]
			break
		}
	}

	if bvid == "" {
		return nil, errors.New("无法解析视频ID")
	}

	// 使用API获取视频信息
	client := resty.New()
	apiResp, err := client.R().
		SetHeader(HttpHeaderUserAgent, DefaultUserAgent).
		SetHeader(HttpHeaderReferer, "https://www.bilibili.com").
		Get(fmt.Sprintf("https://api.bilibili.com/x/web-interface/view?bvid=%s", bvid))
	if err != nil {
		return nil, err
	}

	data := gjson.Parse(string(apiResp.Body()))
	if data.Get("code").Int() != 0 {
		return nil, fmt.Errorf("获取视频信息失败: %s", data.Get("message").String())
	}

	videoData := data.Get("data")
	cid := videoData.Get("cid").String()

	// 获取播放地址（添加了cid参数）
	playResp, err := client.R().
		SetHeader(HttpHeaderUserAgent, DefaultUserAgent).
		SetHeader(HttpHeaderReferer, fmt.Sprintf("https://www.bilibili.com/video/%s", bvid)).
		Get(fmt.Sprintf("https://api.bilibili.com/x/player/wbi/playurl?bvid=%s&cid=%s&qn=80&fnval=4048&fourk=1", bvid, cid))
	if err != nil {
		return nil, err
	}

	playData := gjson.Parse(string(playResp.Body()))
	if playData.Get("code").Int() != 0 {
		return nil, fmt.Errorf("获取播放地址失败: %s", playData.Get("message").String())
	}

	// 获取最高质量的视频地址
	videoUrl := playData.Get("data.dash.video.0.baseUrl").String()
	audioUrl := playData.Get("data.dash.audio.0.baseUrl").String()

	info := &VideoParseInfo{
		Title:    videoData.Get("title").String(),
		VideoUrl: videoUrl,
		MusicUrl: audioUrl,  // 添加音频地址
		CoverUrl: videoData.Get("pic").String(),
	}

	info.Author.Name = videoData.Get("owner.name").String()
	info.Author.Uid = videoData.Get("owner.mid").String()
	info.Author.Avatar = videoData.Get("owner.face").String()

	return info, nil
}
