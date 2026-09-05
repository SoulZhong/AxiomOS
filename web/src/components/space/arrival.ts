/**
 * 登舰抵达的小状态源：AppShell 用 useSyncExternalStore 订阅它，在 layout effect 里读 sessionStorage 标记并切换，
 * 避免在 effect 里同步 setState。不依赖 three。
 */
let arriving = false;
const listeners = new Set<() => void>();
export const arrival = {
  subscribe(cb: () => void) {
    listeners.add(cb);
    return () => {
      listeners.delete(cb);
    };
  },
  get: () => arriving,
  getServer: () => false,
  set(v: boolean) {
    if (arriving === v) return;
    arriving = v;
    listeners.forEach((cb) => cb());
  },
};
