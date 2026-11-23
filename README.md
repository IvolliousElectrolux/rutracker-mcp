# rutracker-mcp

单个 Windows 可执行文件, 用 stdio 提供 RuTracker MCP. 可以搜索, 看主题, 列文件, 取 magnet, 并把 `.torrent` 写到本机目录. 不需要 Docker. 种子正文不会放进模型上下文, 下载工具只返回绝对路径, 字节数和 sha256.

stdout 只留给 MCP. 启动失败和运行日志写到 stderr.

## 工具

- `rutracker_search`: 关键词搜索. 参数 `query`, `forumId`, `sort` (`seeders` / `size` / `date` / `downloads`), `order` (`desc` / `asc`), `limit` (0 表示保留这一页的全部结果).
- `rutracker_topic`: 主题详情, 含标题, 大小, 做种, 下载数, magnet 和 info-hash.
- `rutracker_files`: 不下载种子, 列出文件和精确字节.
- `rutracker_magnet`: 只返回 magnet 和 info-hash.
- `rutracker_download`: 把 `{topicId}.torrent` 写到本机目录, 返回绝对路径, 字节数和 sha256. 不使用站点返回的文件名做路径.

## 环境变量

启动时必须有账号密码, 或 cookie. 否则进程直接退出.

- `RUTRACKER_USERNAME` / `RUTRACKER_PASSWORD`: 登录表单. 遇到验证码时这次登录会失败.
- `RUTRACKER_COOKIE`: 浏览器里的 `bb_session=...`. 出现验证码时改用这项.
- `RUTRACKER_COOKIE_FILE`: 会话保存路径. 默认 `%USERPROFILE%\.rutracker-mcp\cookies.json`.
- `RUTRACKER_BASE_URL`: 站点根地址. 为空时在 `https://rutracker.org/forum/` 和 `https://rutracker.net/forum/` 之间失败切换. 国内建议固定为 `https://rutracker.net/forum/`.
- `RUTRACKER_PROXY`: `http`, `https` 或 `socks5`. 这是本机进程, 代理写 `127.0.0.1`, 不要写 `host.docker.internal`.
- `RUTRACKER_USER_AGENT`: 可选, 覆盖默认浏览器 UA.
- `RUTRACKER_DOWNLOAD_DIR`: `.torrent` 目录. 默认 `%USERPROFILE%\.rutracker-mcp\torrents`.

## Cursor

编辑 `C:\Users\Hillya\.cursor\mcp.json`. 下面保留了现有的 CNKICrawlerMCP. 用户名和密码留空, 在本机填好后再启用. `command` 指向 Actions 编出的 exe.

```json
{
  "mcpServers": {
    "CNKICrawlerMCP": {
      "command": "E:\\git-repo\\CNKICrawlerustMCP\\CNKICrawlerustMCP.exe"
    },
    "rutracker-mcp": {
      "command": "E:\\git-repo\\rutracker-mcp\\dist\\rutracker-mcp.exe",
      "env": {
        "RUTRACKER_USERNAME": "",
        "RUTRACKER_PASSWORD": "",
        "RUTRACKER_BASE_URL": "https://rutracker.net/forum/",
        "RUTRACKER_PROXY": "socks5://127.0.0.1:7890"
      }
    }
  }
}
```

不需要代理时删掉 `RUTRACKER_PROXY`. 不要把它留成空字符串.

## 编译

推送到 GitHub 后, Actions 在 Ubuntu 上交叉编译 Windows amd64 的 `rutracker-mcp.exe`, 并上传 artifact. 本机不要运行 `go build` 或 `go test`. 语法和 gofmt 也在 Actions 里检查.
