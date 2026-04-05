package captcha

import (
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"main/internal/cache"
	"main/internal/logger"

	"github.com/golang/freetype/truetype"
	"github.com/google/uuid"
	"github.com/wenlng/go-captcha-assets/bindata/chars"
	"github.com/wenlng/go-captcha-assets/resources/fonts/fzshengsksjw"
	"github.com/wenlng/go-captcha-assets/resources/imagesv2"
	"github.com/wenlng/go-captcha-assets/resources/tiles"
	"github.com/wenlng/go-captcha/v2/base/option"
	"github.com/wenlng/go-captcha/v2/click"
	"github.com/wenlng/go-captcha/v2/rotate"
	"github.com/wenlng/go-captcha/v2/slide"
)

/*
 * captcha 包封装 go-captcha v2 的点选/滑动/旋转三种验证码
 * 功能：初始化资源、生成验证码图片、缓存验证数据、校验用户提交
 */

const (
	cacheKeyPrefix = "captcha:"
	cacheTTL       = 5 * time.Minute
)

var (
	clickCapt  click.Captcha
	slideCapt  slide.Captcha
	rotateCapt rotate.Captcha
	initOnce   sync.Once
	initErr    error
)

/*
 * Init 初始化所有验证码实例
 * 功能：加载嵌入式字体和图片资源，构建 click/slide/rotate 验证码实例
 */
func Init() error {
	initOnce.Do(func() {
		initErr = doInit()
	})
	return initErr
}

func doInit() error {
	/* 加载字体 */
	fonts, err := fzshengsksjw.GetFont()
	if err != nil {
		return fmt.Errorf("加载字体失败: %w", err)
	}

	/* 加载背景图片 */
	imgs, err := imagesv2.GetImages()
	if err != nil {
		return fmt.Errorf("加载背景图片失败: %w", err)
	}

	/* ===== 点选验证码 ===== */
	clickBuilder := click.NewBuilder(
		click.WithRangeLen(option.RangeVal{Min: 4, Max: 6}),
		click.WithRangeVerifyLen(option.RangeVal{Min: 2, Max: 4}),
	)
	clickBuilder.SetResources(
		click.WithChars(chars.GetChineseChars()),
		click.WithFonts([]*truetype.Font{fonts}),
		click.WithBackgrounds(imgs),
	)
	clickCapt = clickBuilder.Make()

	/* ===== 滑动验证码 ===== */
	tileGraphs, err := tiles.GetTiles()
	if err != nil {
		return fmt.Errorf("加载滑块图片失败: %w", err)
	}
	var slideGraphs = make([]*slide.GraphImage, 0, len(tileGraphs))
	for _, g := range tileGraphs {
		slideGraphs = append(slideGraphs, &slide.GraphImage{
			OverlayImage: g.OverlayImage,
			MaskImage:    g.MaskImage,
			ShadowImage:  g.ShadowImage,
		})
	}
	slideBuilder := slide.NewBuilder()
	slideBuilder.SetResources(
		slide.WithGraphImages(slideGraphs),
		slide.WithBackgrounds(imgs),
	)
	slideCapt = slideBuilder.Make()

	/* ===== 旋转验证码 ===== */
	rotateBuilder := rotate.NewBuilder()
	rotateBuilder.SetResources(
		rotate.WithImages(imgs),
	)
	rotateCapt = rotateBuilder.Make()

	logger.Info("[Captcha] 行为验证码资源初始化完成")
	return nil
}

/* ===== 生成结果结构 ===== */

/* CaptchaResult 验证码生成结果（返回给前端） */
type CaptchaResult struct {
	CaptchaID   string `json:"captcha_id"`
	CaptchaType string `json:"captcha_type"` // click, slide, rotate
	MasterImage string `json:"master_image"` // base64 主图
	ThumbImage  string `json:"thumb_image"`  // base64 缩略图/滑块图
	ThumbX      int    `json:"thumb_x,omitempty"`
	ThumbY      int    `json:"thumb_y,omitempty"`
	ThumbWidth  int    `json:"thumb_width,omitempty"`
	ThumbHeight int    `json:"thumb_height,omitempty"`
	ThumbSize   int    `json:"thumb_size,omitempty"`
}

/* ===== 点选验证码数据缓存结构 ===== */
type clickCacheData struct {
	Dots map[int]*click.Dot `json:"dots"`
}

type slideCacheData struct {
	X      int `json:"x"`
	Y      int `json:"y"`
	Width  int `json:"width"`
	Height int `json:"height"`
}

type rotateCacheData struct {
	Angle int `json:"angle"`
}

/*
 * Generate 生成指定类型的验证码
 * 功能：根据 captchaType 生成对应验证码，存缓存，返回图片数据
 */
func Generate(captchaType string) (*CaptchaResult, error) {
	if err := Init(); err != nil {
		return nil, fmt.Errorf("验证码未初始化: %w", err)
	}

	captchaID := uuid.New().String()

	switch captchaType {
	case "click":
		return generateClick(captchaID)
	case "slide":
		return generateSlide(captchaID)
	case "rotate":
		return generateRotate(captchaID)
	default:
		return generateClick(captchaID)
	}
}

func generateClick(id string) (*CaptchaResult, error) {
	captData, err := clickCapt.Generate()
	if err != nil {
		return nil, fmt.Errorf("生成点选验证码失败: %w", err)
	}

	dotData := captData.GetData()
	if dotData == nil {
		return nil, fmt.Errorf("点选验证码数据为空")
	}

	/* 缓存验证数据 */
	cacheData := &clickCacheData{Dots: dotData}
	storeCache(id, "click", cacheData)

	masterB64, err := captData.GetMasterImage().ToBase64()
	if err != nil {
		return nil, err
	}
	thumbB64, err := captData.GetThumbImage().ToBase64()
	if err != nil {
		return nil, err
	}

	return &CaptchaResult{
		CaptchaID:   id,
		CaptchaType: "click",
		MasterImage: masterB64,
		ThumbImage:  thumbB64,
	}, nil
}

func generateSlide(id string) (*CaptchaResult, error) {
	captData, err := slideCapt.Generate()
	if err != nil {
		return nil, fmt.Errorf("生成滑动验证码失败: %w", err)
	}

	blockData := captData.GetData()
	if blockData == nil {
		return nil, fmt.Errorf("滑动验证码数据为空")
	}

	/* 缓存验证数据 */
	cacheData := &slideCacheData{
		X:      blockData.X,
		Y:      blockData.Y,
		Width:  blockData.Width,
		Height: blockData.Height,
	}
	storeCache(id, "slide", cacheData)

	masterB64, err := captData.GetMasterImage().ToBase64()
	if err != nil {
		return nil, err
	}
	tileB64, err := captData.GetTileImage().ToBase64()
	if err != nil {
		return nil, err
	}

	return &CaptchaResult{
		CaptchaID:   id,
		CaptchaType: "slide",
		MasterImage: masterB64,
		ThumbImage:  tileB64,
		ThumbX:      0,
		ThumbY:      blockData.TileY,
		ThumbWidth:  blockData.Width,
		ThumbHeight: blockData.Height,
	}, nil
}

func generateRotate(id string) (*CaptchaResult, error) {
	captData, err := rotateCapt.Generate()
	if err != nil {
		return nil, fmt.Errorf("生成旋转验证码失败: %w", err)
	}

	blockData := captData.GetData()
	if blockData == nil {
		return nil, fmt.Errorf("旋转验证码数据为空")
	}

	/* 缓存验证数据 */
	cacheData := &rotateCacheData{Angle: blockData.Angle}
	storeCache(id, "rotate", cacheData)

	masterB64, err := captData.GetMasterImage().ToBase64()
	if err != nil {
		return nil, err
	}
	thumbB64, err := captData.GetThumbImage().ToBase64()
	if err != nil {
		return nil, err
	}

	return &CaptchaResult{
		CaptchaID:   id,
		CaptchaType: "rotate",
		MasterImage: masterB64,
		ThumbImage:  thumbB64,
		ThumbSize:   blockData.Width,
	}, nil
}

/* ===== 验证 ===== */

/* ClickPoint 用户提交的点选坐标 */
type ClickPoint struct {
	X int `json:"x"`
	Y int `json:"y"`
}

/*
 * Verify 验证用户提交的验证码答案
 * 功能：从缓存读取验证数据，根据类型执行不同的校验逻辑，一次性使用
 */
func Verify(captchaID string, captchaType string, answer json.RawMessage) bool {
	cacheKey := cacheKeyPrefix + captchaID
	raw, ok := cache.C.Get(cacheKey)
	if !ok {
		return false
	}
	/* 一次性使用 */
	cache.C.Delete(cacheKey)

	switch captchaType {
	case "click":
		return verifyClick(raw, answer)
	case "slide":
		return verifySlide(raw, answer)
	case "rotate":
		return verifyRotate(raw, answer)
	default:
		return false
	}
}

func verifyClick(raw string, answer json.RawMessage) bool {
	var cached clickCacheData
	if err := json.Unmarshal([]byte(raw), &cached); err != nil {
		logger.Error("[Captcha] 解析点选缓存失败: %v", err)
		return false
	}

	var points []ClickPoint
	if err := json.Unmarshal(answer, &points); err != nil {
		logger.Error("[Captcha] 解析点选答案失败: %v", err)
		return false
	}

	if len(points) != len(cached.Dots) {
		return false
	}

	/* 逐点验证坐标是否在容差范围内 */
	for i, pt := range points {
		dot, exists := cached.Dots[i]
		if !exists {
			return false
		}
		if !click.Validate(pt.X, pt.Y, dot.X, dot.Y, dot.Width, dot.Height, 15) {
			return false
		}
	}
	return true
}

func verifySlide(raw string, answer json.RawMessage) bool {
	var cached slideCacheData
	if err := json.Unmarshal([]byte(raw), &cached); err != nil {
		logger.Error("[Captcha] 解析滑动缓存失败: %v", err)
		return false
	}

	var userAnswer struct {
		X int `json:"x"`
		Y int `json:"y"`
	}
	if err := json.Unmarshal(answer, &userAnswer); err != nil {
		logger.Error("[Captcha] 解析滑动答案失败: %v", err)
		return false
	}

	/* 允许 ±5px 误差 */
	ok := abs(userAnswer.X-cached.X) <= 5 && abs(userAnswer.Y-cached.Y) <= 5
	if !ok {
		logger.Debug("[Captcha] 滑动验证偏差: 用户(%d,%d) 正确(%d,%d) dX=%d dY=%d",
			userAnswer.X, userAnswer.Y, cached.X, cached.Y,
			abs(userAnswer.X-cached.X), abs(userAnswer.Y-cached.Y))
	}
	return ok
}

func verifyRotate(raw string, answer json.RawMessage) bool {
	var cached rotateCacheData
	if err := json.Unmarshal([]byte(raw), &cached); err != nil {
		logger.Error("[Captcha] 解析旋转缓存失败: %v", err)
		return false
	}

	var userAnswer struct {
		Angle int `json:"angle"`
	}
	if err := json.Unmarshal(answer, &userAnswer); err != nil {
		logger.Error("[Captcha] 解析旋转答案失败: %v", err)
		return false
	}
	sum := (userAnswer.Angle + cached.Angle) % 360
	if sum < 0 {
		sum += 360
	}
	ok := sum <= 5 || sum >= 355
	if !ok {
		logger.Debug("[Captcha] 旋转验证偏差: 用户=%d° 预旋转=%d° sum=%d°",
			userAnswer.Angle, cached.Angle, sum)
	}
	return ok
}

/* ===== 缓存操作 ===== */

func storeCache(id, captchaType string, data interface{}) {
	cacheKey := cacheKeyPrefix + id
	wrapper := map[string]interface{}{
		"type": captchaType,
	}
	raw, err := json.Marshal(data)
	if err != nil {
		logger.Error("[Captcha] 序列化缓存数据失败: %v", err)
		return
	}
	if err := json.Unmarshal(raw, &wrapper); err != nil {
		logger.Error("[Captcha] 展开缓存数据失败: %v", err)
		return
	}

	cache.C.SetJSON(cacheKey, wrapper, cacheTTL)
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}
