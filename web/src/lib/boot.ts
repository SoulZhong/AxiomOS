// 首屏内联脚本（layout.tsx 注入 <head>）：在水合之前把主题与侧栏偏好写到 <html> 上，避免闪一下深色 / 展开态。
// 这个文件没有 "use client"：服务端组件 layout.tsx 要拿到的是字符串本身；从 "use client" 模块导入只会拿到客户端引用代理
// （渲染成 function(){throw Error(...)}，脚本报错、偏好不生效）。theme.ts / sidebar.ts 从这里取键名。
export const THEME_KEY = "axiomos.theme";
export const SIDEBAR_KEY = "axiomos.sidebar";

export const THEME_BOOT_SCRIPT = `try{var t=localStorage.getItem(${JSON.stringify(THEME_KEY)});if(t==="dark"||t==="light")document.documentElement.setAttribute("data-theme",t)}catch(e){}`;
export const SIDEBAR_BOOT_SCRIPT = `try{var s=localStorage.getItem(${JSON.stringify(SIDEBAR_KEY)});if(s==="collapsed"||s==="expanded")document.documentElement.setAttribute("data-sidebar",s)}catch(e){}`;
export const BOOT_SCRIPT = THEME_BOOT_SCRIPT + SIDEBAR_BOOT_SCRIPT;
