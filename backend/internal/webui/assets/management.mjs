// Family membership and device settings UI. Lifecycle ownership stays in app.mjs.
export function createManagement({
  request,
  element,
  generation,
  active,
  onSelect,
  onChanged,
  onView,
  onError,
  onRevoke,
}) {
  const $ = (id) => document.getElementById(id);
  let families = [],
    selected = null,
    target = null,
    editGeneration = 0,
    ownedDevices = [],
    ready = false,
    familyAction = 0,
    pendingDelete = null,
    familyBusy = false;
  const valid = (epoch) => active() && generation() === epoch;
  function familyPath(path) {
    if (!ready) throw new Error("view not initialized");
    return selected
      ? `${path}?family=${encodeURIComponent(selected.public_id)}`
      : `${path}?scope=owned`;
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
  function clearPasscode() {
    $("current-passcode").value = "";
    $("family-passcode").value = "";
    $("passcode-result").hidden = true;
    $("passcode-copy-status").textContent =
      "口令仅在此次设置成功后显示，请妥善保存并单独分享。";
  }
  function showPasscode(value) {
    $("current-passcode").value = value;
    $("passcode-result").hidden = false;
  }
  function renderSelection() {
    const all = element("option", "", "所有设备");
    all.value = "";
    const options = [
      all,
      ...families.map((f) => {
        const o = element("option", "", f.name);
        o.value = f.public_id;
        return o;
      }),
    ];
    if (!ready) {
      const empty = element("option", "", "请选择有权限的家庭或所有设备");
      empty.value = "invalid";
      options.unshift(empty);
    }
    $("family-select").replaceChildren(...options);
    $("family-select").value = ready ? selected?.public_id || "" : "invalid";
    $("generate-pairing").disabled = !selected;
    $("pairing-family-hint").textContent = selected
      ? `配对设备将加入“${selected.name}”。`
      : "请先选择目标家庭；还没有家庭时，请先创建家庭。";
    $("family-settings").hidden = !selected;
    $("copy-family").disabled = !selected?.passcode_set;
    $("family-link").value = selected
      ? `${location.origin}/family?key=${encodeURIComponent(selected.public_id)}`
      : "";
    $("family-link-status").textContent = selected
      ? selected.passcode_set
        ? "查看需输入家庭口令，同一浏览器验证后 7 天内免输入。"
        : "待设置口令：请先生成或手动设置，再分享家庭链接。"
      : "选择家庭后可管理名称、口令并复制查看链接。";
    $("family-passcode-status").textContent = selected?.passcode_set
      ? "口令已设置，旧口令无法回看；重新设置会使已有家庭访问凭证失效。"
      : "待设置口令：旧家庭需要先设置口令才能分享。";
    $("rename-family-name").value = selected?.name || "";
    $("devices-heading").textContent = selected ? "当前家庭成员" : "所有设备";
    $("owned-devices-card").hidden = !selected;
  }
  function select(id, updateURL = true) {
    familyAction++;
    pendingDelete = null;
    $("family-confirm-overlay").hidden = true;
    selected = families.find((f) => f.public_id === id) || null;
    ready = id === "" || Boolean(selected);
    clearEditor();
    clearPasscode();
    renderSelection();
    if (ready && updateURL) {
      const url = new URL(location.href);
      if (selected) url.searchParams.set("family", selected.public_id);
      else url.searchParams.delete("family");
      history.replaceState(null, "", url);
    }
  }
  async function load() {
    const epoch = generation();
    const result = await request("/api/v1/families");
    if (!valid(epoch)) return false;
    if (!Array.isArray(result.families)) throw new Error("invalid families");
    families = result.families;
    const keys = new URL(location.href).searchParams.getAll("family");
    select(
      keys.length === 0 ? "" : keys.length === 1 && keys[0] ? keys[0] : null,
    );
    if (!ready) {
      const error = new Error("family unavailable");
      error.status = 404;
      throw error;
    }
    return true;
  }
  $("family-select").addEventListener("change", () => {
    if (!active()) return;
    select($("family-select").value);
    onSelect();
  });
  $("create-family-form").addEventListener("submit", async (event) => {
    event.preventDefault();
    if (!active() || familyBusy) return;
    const epoch = generation();
    setFamilyBusy(true);
    try {
      const family = await request("/api/v1/families", {
        method: "POST",
        body: {
          name: $("family-name").value.trim(),
          ...($("create-family-passcode").value
            ? { passcode: $("create-family-passcode").value }
            : {}),
        },
      });
      if (!valid(epoch)) return;
      const { passcode, ...record } = family;
      families.push(record);
      $("family-name").value = "";
      $("create-family-passcode").value = "";
      select(family.public_id);
      onSelect();
      showPasscode(passcode);
    } catch (error) {
      if (valid(epoch)) onError(error);
    } finally {
      setFamilyBusy(false);
    }
  });
  $("copy-family").addEventListener("click", async () => {
    if (!active() || !selected?.passcode_set) return;
    const epoch = generation();
    try {
      if (!navigator.clipboard?.writeText)
        throw new Error("clipboard unavailable");
      await navigator.clipboard.writeText($("family-link").value);
      if (valid(epoch))
        $("family-link-status").textContent =
          "家庭链接已复制，请单独分享 7 位数字口令。";
    } catch {
      if (!valid(epoch)) return;
      $("family-link").focus();
      $("family-link").select();
      $("family-link-status").textContent = "请长按上方链接，选择并复制。";
    }
  });
  function setFamilyBusy(busy) {
    familyBusy = busy;
    for (const id of [
      "create-family",
      "rename-family",
      "save-family-passcode",
      "regenerate-passcode",
      "delete-family",
      "confirm-delete-family",
      "cancel-delete-family",
    ])
      $(id).disabled = busy;
  }
  async function familyWrite(button, path, method, body, success) {
    if (!active() || !selected || familyBusy || button.disabled) return;
    const epoch = generation(),
      action = familyAction;
    setFamilyBusy(true);
    try {
      const result = await request(
        `/api/v1/families/${encodeURIComponent(selected.public_id)}${path}`,
        { method, ...(body === undefined ? {} : { body }) },
      );
      if (valid(epoch) && action === familyAction) success(result);
    } catch (error) {
      if (valid(epoch) && action === familyAction) onError(error);
    } finally {
      setFamilyBusy(false);
    }
  }
  $("rename-family-form").addEventListener("submit", (event) => {
    event.preventDefault();
    const name = $("rename-family-name").value.trim();
    if (!name) return;
    familyWrite($("rename-family"), "", "PATCH", { name }, () => {
      selected.name = name;
      renderSelection();
      onChanged("家庭名称已修改。");
    });
  });
  function setPasscode(automatic) {
    const passcode = $("family-passcode").value;
    if (!automatic && !/^[0-9]{7}$/.test(passcode)) {
      onError({ status: 400 });
      return;
    }
    familyWrite(
      $(automatic ? "regenerate-passcode" : "save-family-passcode"),
      "/passcode",
      "PUT",
      automatic ? {} : { passcode },
      (result) => {
        selected.passcode_set = true;
        renderSelection();
        clearPasscode();
        showPasscode(result.passcode);
        onChanged("家庭口令已设置，旧访问凭证已失效。");
      },
    );
  }
  $("family-passcode-form").addEventListener("submit", (event) => {
    event.preventDefault();
    setPasscode(false);
  });
  $("regenerate-passcode").addEventListener("click", () => setPasscode(true));
  $("copy-passcode").addEventListener("click", async () => {
    if (!active() || $("passcode-result").hidden) return;
    const epoch = generation();
    try {
      if (!navigator.clipboard?.writeText) throw new Error();
      await navigator.clipboard.writeText($("current-passcode").value);
      if (valid(epoch))
        $("passcode-copy-status").textContent = "口令已复制，请单独分享。";
    } catch {
      if (valid(epoch)) {
        $("current-passcode").focus();
        $("current-passcode").select();
        $("passcode-copy-status").textContent = "请长按上方口令，选择并复制。";
      }
    }
  });
  function closeDelete() {
    pendingDelete = null;
    $("family-confirm-overlay").hidden = true;
    $("delete-family").focus();
  }
  $("delete-family").addEventListener("click", () => {
    if (!active() || !selected) return;
    pendingDelete = selected.public_id;
    $("family-delete-description").textContent =
      `删除“${selected.name}”后，家庭链接与配对信息失效。设备仍保留并可继续上报，其他家庭不受影响。`;
    $("family-confirm-overlay").hidden = false;
    $("cancel-delete-family").focus();
  });
  $("cancel-delete-family").addEventListener("click", closeDelete);
  $("family-confirm-overlay").addEventListener("keydown", (event) => {
    if (event.key === "Escape" && !$("confirm-delete-family").disabled)
      closeDelete();
    if (event.key === "Tab") {
      event.preventDefault();
      (document.activeElement === $("cancel-delete-family")
        ? $("confirm-delete-family")
        : $("cancel-delete-family")
      ).focus();
    }
  });
  $("confirm-delete-family").addEventListener("click", () => {
    if (!selected || pendingDelete !== selected.public_id) return;
    const key = selected.public_id;
    familyWrite($("confirm-delete-family"), "", "DELETE", undefined, () => {
      families = families.filter((f) => f.public_id !== key);
      select("");
      onChanged("家庭已删除，设备仍保留在所有设备中。");
    });
  });
  async function membership(device, add) {
    if (!active() || !selected) return;
    const epoch = generation();
    try {
      await request(
        `/api/v1/families/${encodeURIComponent(selected.public_id)}/devices/${encodeURIComponent(device.id)}`,
        { method: add ? "PUT" : "DELETE", ...(add ? { body: {} } : {}) },
      );
      if (valid(epoch))
        onChanged(
          add
            ? "设备已加入当前家庭。"
            : "设备已移出当前家庭，其他家庭和上报不受影响。",
        );
    } catch (error) {
      if (valid(epoch)) onError(error);
    }
  }
  async function configure(device) {
    if (!active()) return;
    clearEditor();
    target = device;
    const edit = editGeneration,
      epoch = generation();
    onView("settings");
    $("config-device-name").textContent = `${device.name} · 设备配置`;
    const memberships =
      ownedDevices.find((d) => d.id === device.id)?.families || [];
    $("config-device-families").textContent = memberships.length
      ? `所属家庭：${memberships.map((f) => f.name).join("、")}`
      : "尚未加入家庭，可在设备管理中添加。";
    $("config-status").textContent = "正在读取设备配置…";
    try {
      const c = await request(
        `/api/v1/devices/${encodeURIComponent(device.id)}/config`,
      );
      if (!valid(epoch) || edit !== editGeneration) return;
      $("moving-minutes").value = c.moving_interval_seconds / 60;
      $("stationary-minutes").value = c.stationary_interval_seconds / 60;
      $("moving-high-accuracy").checked = c.moving_high_accuracy;
      $("quiet-start").value = c.quiet_hours_start;
      $("quiet-end").value = c.quiet_hours_end;
      $("config-fields").disabled = false;
      $("config-status").textContent =
        `服务器配置版本 ${c.version}。设备将在成功上报时获取配置。`;
    } catch (error) {
      if (valid(epoch) && edit === editGeneration) {
        $("config-status").textContent = "配置读取失败，请返回后重试。";
        onError(error);
      }
    }
  }
  $("device-config-form").addEventListener("submit", async (event) => {
    event.preventDefault();
    if (
      !active() ||
      !target ||
      $("config-fields").disabled ||
      $("save-device-config").disabled
    )
      return;
    const moving = Number($("moving-minutes").value),
      stationary = Number($("stationary-minutes").value);
    const start = $("quiet-start").value,
      end = $("quiet-end").value;
    if (
      !Number.isInteger(moving) ||
      !Number.isInteger(stationary) ||
      moving < 1 ||
      stationary < moving ||
      stationary > 60 ||
      !start ||
      !end ||
      start === end
    ) {
      $("config-status").textContent =
        "请填写 1–60 分钟，静止间隔不小于移动间隔，暂停开始与结束时间不能相同。";
      return;
    }
    const epoch = generation(),
      edit = editGeneration,
      device = target;
    $("save-device-config").disabled = true;
    try {
      const c = await request(
        `/api/v1/devices/${encodeURIComponent(device.id)}/config`,
        {
          method: "PUT",
          body: {
            moving_interval_seconds: moving * 60,
            stationary_interval_seconds: stationary * 60,
            moving_high_accuracy: $("moving-high-accuracy").checked,
            quiet_hours_start: start,
            quiet_hours_end: end,
          },
        },
      );
      if (valid(epoch) && edit === editGeneration)
        $("config-status").textContent =
          `已保存（版本 ${c.version}），下次设备上报后获取。已暂停或离线的设备需按原配置恢复并成功上报后才能更新。`;
    } catch (error) {
      if (valid(epoch) && edit === editGeneration) {
        $("config-status").textContent = "保存失败，输入已保留，请检查后重试。";
        onError(error);
      }
    } finally {
      if (edit === editGeneration) $("save-device-config").disabled = false;
    }
  });
  $("back-devices").addEventListener("click", () => {
    clearEditor();
    onView("devices");
  });
  function button(text, action, className = "text-button") {
    const b = element("button", className, text);
    b.type = "button";
    b.addEventListener("click", action);
    return b;
  }
  function renderOwned(devices, members) {
    ownedDevices = devices;
    const ids = new Set(members.map((d) => d.id));
    const rows = devices
      .filter((d) => !d.revoked_at)
      .map((d) => {
        const row = element("div", "device-row");
        row.dataset.deviceId = d.id;
        const info = element("div", "device-info");
        info.append(
          element("h3", "", d.name),
          element("p", "", ids.has(d.id) ? "已在当前家庭" : "未加入当前家庭"),
        );
        const actions = element("div", "device-actions");
        const revoke = button("撤销", () => onRevoke(d, revoke));
        actions.append(revoke);
        actions.append(
          button("配置", () => configure(d), "text-button config-device"),
        );
        if (!ids.has(d.id) && selected)
          actions.append(
            button(
              "加入当前家庭",
              () => membership(d, true),
              "text-button add-member",
            ),
          );
        row.append(info, actions);
        return row;
      });
    $("owned-devices-list").replaceChildren(...rows);
  }
  function offline() {
    editGeneration++;
    $("save-device-config").disabled = false;
    if (target)
      $("config-status").textContent =
        "网络中断，无法确认保存结果；输入已保留，请联网后核对或重试保存。";
  }
  function reset() {
    clearEditor();
    clearPasscode();
    families = [];
    selected = null;
    ready = false;
    familyAction++;
    pendingDelete = null;
    $("family-confirm-overlay").hidden = true;
    ownedDevices = [];
    renderSelection();
    $("owned-devices-list").replaceChildren();
    $("family-name").value = "";
    $("create-family-passcode").value = "";
  }
  return {
    load,
    familyPath,
    get familyId() {
      return selected?.public_id;
    },
    get ready() {
      return ready;
    },
    renderOwned,
    membership,
    configure,
    clearEditor,
    offline,
    reset,
  };
}
