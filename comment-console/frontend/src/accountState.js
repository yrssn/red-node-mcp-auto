export function emptyAccount() {
  return {
    id: "",
    name: "",
    logged_in: null,
    checked_at: 0,
    checking: false,
    error: "",
  };
}

export function mergeAccountStatus(previous, result, fresh = false) {
  const hasCheckedAt =
    result.checked_at !== undefined && result.checked_at !== null;
  const checkedAt = hasCheckedAt
    ? Number(result.checked_at) || 0
    : fresh && !result.error
    ? Math.floor(Date.now() / 1000)
    : 0;
  const confirmed =
    checkedAt > 0 &&
    !result.error &&
    !result.checking &&
    typeof result.logged_in === "boolean";
  return {
    ...previous,
    id: result.id || previous.id || "",
    name: result.name || previous.name || "",
    // 未核实的缓存或检查失败，保留上次已知身份。
    logged_in: confirmed ? !!result.logged_in : previous.logged_in,
    checked_at: checkedAt,
    checking: !!result.checking,
    error: result.error || "",
  };
}

export function accountPhase(account, busy = false) {
  if (busy || account.checking) return "checking";
  if (account.error) return "uncertain";
  if (!account.checked_at || typeof account.logged_in !== "boolean")
    return "unknown";
  return account.logged_in ? "online" : "offline";
}
