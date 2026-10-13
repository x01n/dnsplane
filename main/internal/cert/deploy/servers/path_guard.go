package servers

import (
	"fmt"
	"path/filepath"
	"strings"
)

/*
 * 安全审计 H-1：部署器在证书写入目标路径时，原实现对用户填写的 cert_path / key_path
 * 不做任何校验，允许 `../../etc/cron.d/x` 类路径遍历。此文件提供统一的路径安全网。
 *
 * 防御规则：
 *   1. 拒绝包含 `..` 段的路径（即便在 Clean 之后仍有），阻断上溯
 *   2. 拒绝 NUL / 控制字符
 *   3. SSH/FTP 远端路径：强制以 `/` 开头（POSIX 绝对路径）
 *   4. local 写入路径：强制以可识别绝对路径起始（POSIX `/` 或 Windows 盘符）
 *   5. 不对路径做归一化改写，仅做校验，避免把用户本意为 `{domain}` 占位的 `../x` 误判
 */

// sanitizeRemotePath 校验 SSH / FTP 等远端 POSIX 路径。
func sanitizeRemotePath(field, raw string) (string, error) {
	return sanitizeWithMode(field, raw, true)
}

// sanitizeLocalPath 校验本机文件系统路径（兼容 Windows 盘符）。
func sanitizeLocalPath(field, raw string) (string, error) {
	return sanitizeWithMode(field, raw, false)
}

func sanitizeWithMode(field, raw string, remote bool) (string, error) {
	p := strings.TrimSpace(raw)
	if p == "" {
		return "", fmt.Errorf("%s 不能为空", field)
	}
	// 拒绝 NUL / 控制字符（\x00-\x1f），防止与路径/命令的协议边界混淆
	for _, r := range p {
		if r < 0x20 || r == 0x7f {
			return "", fmt.Errorf("%s 包含控制字符", field)
		}
	}
	// 显式拒绝 .. 段（允许 `{domain}` 占位符，但禁止上溯）
	// 用 / 与 \ 两种分隔符都拆分一次覆盖 Windows 路径写法
	for _, sep := range []rune{'/', '\\'} {
		for _, seg := range strings.Split(p, string(sep)) {
			if seg == ".." {
				return "", fmt.Errorf("%s 不允许包含上溯片段 \"..\"", field)
			}
		}
	}
	if remote {
		// 远端 POSIX 路径：必须 `/` 起始
		if !strings.HasPrefix(p, "/") {
			return "", fmt.Errorf("%s 必须为绝对路径（以 / 开头）", field)
		}
	} else {
		// 本机：允许 POSIX 绝对路径或 Windows 盘符路径
		if !filepath.IsAbs(p) && !strings.HasPrefix(p, "/") {
			return "", fmt.Errorf("%s 必须为绝对路径", field)
		}
	}
	return p, nil
}
