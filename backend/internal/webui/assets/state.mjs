export function estimateServerTime(clock, now = performance.now()) {
  return clock.serverTime + Math.max(0, now - clock.receivedAt);
}
export function locationStatus(point, now, staleAfter = 300) {
  const captured = Date.parse(point.captured_at);
  const expires = Date.parse(point.lease_expires_at);
  if (!Number.isFinite(captured) || !Number.isFinite(expires)) return "invalid";
  if (expires <= now) return "stale";
  return now - captured >= staleAfter * 1000 ? "stale" : "fresh";
}
export function visibleLocations(locations, devices, now) {
  const allowed = new Set(
    devices.filter((d) => !d.revoked_at).map((d) => d.id),
  );
  return locations.filter(
    (p) =>
      allowed.has(p.device_id) &&
      p.coordinate_system === "GCJ-02" &&
      Number.isFinite(p.latitude) &&
      Math.abs(p.latitude) <= 90 &&
      Number.isFinite(p.longitude) &&
      Math.abs(p.longitude) <= 180 &&
      locationStatus(p, now) !== "invalid",
  );
}
export function relativeTime(value, now) {
  const elapsed = Math.max(0, Math.floor((now - Date.parse(value)) / 1000));
  if (!Number.isFinite(elapsed)) return "时间未知";
  if (elapsed < 10) return "刚刚";
  if (elapsed < 60) return `${elapsed} 秒前`;
  if (elapsed < 3600) return `${Math.floor(elapsed / 60)} 分钟前`;
  if (elapsed < 86400) return `${Math.floor(elapsed / 3600)} 小时前`;
  return `${Math.floor(elapsed / 86400)} 天前`;
}
export function remainingSeconds(expires, now) {
  const value = Math.ceil((Date.parse(expires) - now) / 1000);
  return Number.isFinite(value) ? Math.max(0, value) : 0;
}
export function errorMessage(error) {
  if (error.status === 401) return "账号或密码不正确，或登录已过期。";
  if (error.status === 403) return "操作校验失败，请刷新页面后重试。";
  if (error.status === 429) return "操作过于频繁，请稍后再试。";
  if (error.status === 400) return "提交的信息有误，请检查后重试。";
  if (error.status === 404) return "设备不存在或已无法访问，请刷新列表。";
  return "暂时无法连接服务，请检查网络后重试。";
}
