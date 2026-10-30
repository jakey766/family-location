// Family membership and device settings UI. Lifecycle ownership stays in app.mjs.
export function createManagement({ request, element, generation, active, onSelect, onChanged, onView, onError, onRevoke }) {
  const $ = (id) => document.getElementById(id);
  let families = [], selected = null, target = null, editGeneration = 0, ownedDevices = [];
  const valid = (epoch) => active() && generation() === epoch;
  function familyPath(path) {
    if (!selected) throw new Error("family not selected");
    return `${path}?family=${encodeURIComponent(selected.public_id)}`;
  }
  function clearEditor() {
    editGeneration++;
    target = null;
    $("device-config-form").reset();
    $("config-fields").disabled = true;
    $("save-device-config").disabled = false;
    $("config-device-name").textContent = "设备配置";
    $("config-device-families").textContent = "";
    $("config-status").textContent = "";
  }
  function renderSelection() {
    const options = families.map((f) => {
      const o = element("option", "", f.name);
      o.value = f.public_id;
      return o;
    });
    if (!selected) {
      const empty = element("option", "", "请选择有权限的家庭");
      empty.value = "";
      options.unshift(empty);
    }
    $("family-select").replaceChildren(...options);
    $("family-select").value = selected?.public_id || "";
    $("generate-pairing").disabled = !selected;
    $("copy-family").disabled = !selected;
    $("family-link").value = selected ? `${location.origin}/?family=${encodeURIComponent(selected.public_id)}` : "";
    $("family-link-status").textContent = "查看链接仍需登录有权限的账号。";
  }
  function select(id, updateURL = true) {
    selected = families.find((f) => f.public_id === id) || null;
    clearEditor();
    renderSelection();
    if (selected && updateURL) {
      const url = new URL(location.href);
      url.searchParams.set("family", selected.public_id);
      history.replaceState(null, "", url);
    }
  }
  async function load(defaultFamilyID) {
    const epoch = generation();
    const result = await request("/api/v1/families");
    if (!valid(epoch)) return false;
    if (!Array.isArray(result.families)) throw new Error("invalid families");
    families = result.families;
    const params = new URL(location.href).searchParams;
    const keys = params.getAll("family");
    const id = keys.length === 0
      ? families.find((f) => f.id === defaultFamilyID)?.public_id
      : keys.length === 1 ? keys[0] : null;
    select(id);
    if (!selected) { const error = new Error("family unavailable"); error.status = 404; throw error; }
    return true;
  }
  $("family-select").addEventListener("change", () => {
    if (!active()) return;
    select($("family-select").value);
    onSelect();
  });
  $("create-family-form").addEventListener("submit", async (event) => {
    event.preventDefault();
    if (!active()) return;
    const epoch = generation();
    $("create-family").disabled = true;
    try {
      const family = await request("/api/v1/families", {method:"POST",body:{name:$("family-name").value.trim()}});
      if (!valid(epoch)) return;
      families.push(family);
      $("family-name").value = "";
      select(family.public_id);
      onSelect();
    } catch (error) { if (valid(epoch)) onError(error); }
    finally { $("create-family").disabled = false; }
  });
  $("copy-family").addEventListener("click", async () => {
    if (!active() || !selected) return;
    const epoch = generation();
    try {
      if (!navigator.clipboard?.writeText) throw new Error("clipboard unavailable");
      await navigator.clipboard.writeText($("family-link").value);
      if (valid(epoch)) $("family-link-status").textContent = "家庭链接已复制，查看仍需登录有权限的账号。";
    } catch {
      if (!valid(epoch)) return;
      $("family-link").focus(); $("family-link").select();
      $("family-link-status").textContent = "请长按上方链接，选择并复制。";
    }
  });
  async function membership(device, add) {
    if (!active() || !selected) return;
    const epoch = generation();
    try {
      await request(`/api/v1/families/${encodeURIComponent(selected.public_id)}/devices/${encodeURIComponent(device.id)}`, {method:add?"PUT":"DELETE", ...(add?{body:{}}:{})});
      if (valid(epoch)) onChanged(add ? "设备已加入当前家庭。" : "设备已移出当前家庭，其他家庭和上报不受影响。");
    } catch (error) { if (valid(epoch)) onError(error); }
  }
  async function configure(device) {
    if (!active()) return;
    clearEditor();
    target = device;
    const edit = editGeneration, epoch = generation();
    onView("settings");
    $("config-device-name").textContent = `${device.name} · 设备配置`;
    const memberships = ownedDevices.find((d) => d.id === device.id)?.families || [];
    $("config-device-families").textContent = memberships.length ? `所属家庭：${memberships.map((f) => f.name).join("、")}` : "尚未加入家庭，可在设备管理中添加。";
    $("config-status").textContent = "正在读取设备配置…";
    try {
      const c = await request(`/api/v1/devices/${encodeURIComponent(device.id)}/config`);
      if (!valid(epoch) || edit !== editGeneration) return;
      $("moving-minutes").value = c.moving_interval_seconds / 60;
      $("stationary-minutes").value = c.stationary_interval_seconds / 60;
      $("moving-high-accuracy").checked = c.moving_high_accuracy;
      $("quiet-start").value = c.quiet_hours_start;
      $("quiet-end").value = c.quiet_hours_end;
      $("config-fields").disabled = false;
      $("config-status").textContent = `服务器配置版本 ${c.version}。设备将在成功上报时获取配置。`;
    } catch (error) {
      if (valid(epoch) && edit === editGeneration) { $("config-status").textContent = "配置读取失败，请返回后重试。"; onError(error); }
    }
  }
  $("device-config-form").addEventListener("submit", async (event) => {
    event.preventDefault();
    if (!active() || !target || $("config-fields").disabled || $("save-device-config").disabled) return;
    const moving = Number($("moving-minutes").value), stationary = Number($("stationary-minutes").value);
    const start = $("quiet-start").value, end = $("quiet-end").value;
    if (!Number.isInteger(moving) || !Number.isInteger(stationary) || moving < 1 || stationary < moving || stationary > 60 || !start || !end || start === end) {
      $("config-status").textContent = "请填写 1–60 分钟，静止间隔不小于移动间隔，暂停开始与结束时间不能相同。";
      return;
    }
    const epoch = generation(), edit = editGeneration, device = target;
    $("save-device-config").disabled = true;
    try {
      const c = await request(`/api/v1/devices/${encodeURIComponent(device.id)}/config`, {method:"PUT",body:{moving_interval_seconds:moving*60,stationary_interval_seconds:stationary*60,moving_high_accuracy:$("moving-high-accuracy").checked,quiet_hours_start:start,quiet_hours_end:end}});
      if (valid(epoch) && edit === editGeneration) $("config-status").textContent = `已保存（版本 ${c.version}），下次设备上报后获取。已暂停或离线的设备需按原配置恢复并成功上报后才能更新。`;
    } catch (error) {
      if (valid(epoch) && edit === editGeneration) { $("config-status").textContent = "保存失败，输入已保留，请检查后重试。"; onError(error); }
    } finally { if (edit === editGeneration) $("save-device-config").disabled = false; }
  });
  $("back-devices").addEventListener("click", () => { clearEditor(); onView("devices"); });
  function button(text, action, className = "text-button") {
    const b = element("button", className, text); b.type="button"; b.addEventListener("click",action); return b;
  }
  function renderOwned(devices, members) {
    ownedDevices = devices;
    const ids = new Set(members.map((d) => d.id));
    const rows = devices.filter((d) => !d.revoked_at).map((d) => {
      const row = element("div", "device-row"); row.dataset.deviceId = d.id;
      const info = element("div", "device-info");
      info.append(element("h3", "", d.name), element("p", "", ids.has(d.id)?"已在当前家庭":"未加入当前家庭"));
      const actions = element("div", "device-actions");
      const revoke = button("撤销", () => onRevoke(d, revoke)); actions.append(revoke);
      actions.append(button("配置", () => configure(d), "text-button config-device"));
      if (!ids.has(d.id) && selected) actions.append(button("加入当前家庭", () => membership(d,true), "text-button add-member"));
      row.append(info,actions); return row;
    });
    $("owned-devices-list").replaceChildren(...rows);
  }
  function offline() {
    editGeneration++;
    $("save-device-config").disabled = false;
    if (target) $("config-status").textContent = "网络中断，无法确认保存结果；输入已保留，请联网后核对或重试保存。";
  }
  function reset() {
    clearEditor(); families=[]; selected=null; ownedDevices=[]; renderSelection();
    $("owned-devices-list").replaceChildren(); $("family-name").value="";
  }
  return {load, familyPath, get familyId(){return selected?.public_id;}, renderOwned, membership, configure, clearEditor, offline, reset};
}
