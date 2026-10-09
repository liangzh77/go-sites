// Copy this to /srv/sites/liangz77.cn/shared/site-config.js on the server.
// This client-loaded file controls visibility only; its contents are public to visitors.
// NEVER put secrets (SMTP passwords, AppSecret, API keys) in this file.
window.SITE_CONFIG = {
  publicOrigin: "https://liangz77.cn",
  privateArea: {
    password: "replace-with-password"
  },
  // WeChat website-app scan login (open.weixin.qq.com). AppID is public and may live here.
  // Leave appId empty until the WeChat 网站应用 is approved; /login/ then shows a placeholder.
  // The AppSecret must stay server-side and must never appear in this file.
  wechatLogin: {
    appId: "",
    redirectUri: "https://liangz77.cn/api/auth/wechat/callback"
  }
};
