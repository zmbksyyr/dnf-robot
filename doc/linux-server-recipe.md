# Linux 服务端启动配方（供真实 Linux 主机使用）

本文汇总在本环境（WSL2）排查 Linux 版 `df_game_r` 启动问题得到的结论与可复用的启动配方。
**注意**：本环境最终仍无法让服务端进入服务循环（见"已知限制"），配方用于真实 Linux 主机。

## 一、必须避开的坑：不要用加载器启动

原始 `run.sh` 用：

```
ld-linux.so.2 --library-path ... /home/neople/game/df_game_r siroco11 nofork
```

这会让**加载器成为主程序**：它的 BSS 决定 brk 基址，而加载器被 ASLR 放在库区（高位，约 `0xf7ffe000`），
游戏堆一涨就顶到栈（实测堆 167 MB、栈只剩 132 KB），随后第一个大栈帧（约 424 KB）直接 SIGSEGV。

**正确做法**：直接执行游戏二进制，让它成为主程序（基址 `0x08048000`），堆与栈之间就有数 GB 空间：

```
ln -sf /opt/dnfroot/lib/ld-linux.so.2 /lib/ld-linux.so.2   # 游戏 ELF 的 interpreter 就是它
cd /home/neople/game
LD_PRELOAD=/dp2/libdp2pre.so \
LD_LIBRARY_PATH=/dp2/lib:/home/neople/game:/opt/dnfroot/lib:/opt/dnfroot/lib/i686/nosegneg:/opt/dnfroot/usr/lib \
GCONV_PATH=/opt/dnfroot/usr/lib/gconv \
./df_game_r siroco11 nofork
```

## 二、桥接脚本的数据库账号

`/opt/dnfroot/dp2/df_game_r.js` 里数据库密码是占位符 `@DNF_FRIDA_PASSWORD@`，需要替换成实际密码，
并在 MySQL 中创建 `dnf_frida` 用户（`%` 与 `localhost` 两个 host 都建），否则启动时抛
`api_MySQL_exec` 错误。

## 三、worker 线程的 exit 会打死进程

桥接在 UDP 线程里调用 `exit()`，glibc 的 `__run_exit_handlers` 在那里崩溃。垫片做法：非主线程调用
`exit()` 时改成 `pthread_exit`（见 `x/linux-server/heapsim/dnf_exit_guard.c`）。

## 四、已知限制（本环境未解决）

即使以上三点都处理，游戏在打印
`The siroco11 channel 11('ch.11') server has been started successfully` 之后仍以 `exit_group(0)`
**自愿退出**（`strace` 确认无任何信号）。推断需要管理器/父进程常驻拉起通道，属于部署形态问题。
真实 Linux 主机上按原厂部署方式（manager 拉起）应当不会出现此现象。