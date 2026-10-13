package cloudflare

import "main/internal/dns"

/*
 * Cloudflare 解析线路编目
 *
 * 只有两条的原因：Cloudflare 不提供电信/联通/移动这类按运营商分线路的解析，
 * 它的「线路」实际是记录的代理开关——0 表示仅 DNS（灰云，直接回源），
 * 1 表示已代理（橙云，流量经 Cloudflare 边缘）。因此这里不可能是动态列表，
 * 也不需要调 API 查询，恒为下面两条
 *
 * 取值以 GetRecordLine（cloudflare.go）的既有映射和 DNS 记录写入时的判定为准：
 * AddDomainRecord/UpdateDomainRecord 把 line == "1" 翻译成 proxied=true，其余值一律视为仅 DNS
 */
var Lines = []dns.RecordLine{
	{ID: "0", Name: "仅DNS"},
	{ID: "1", Name: "已代理"},
}
