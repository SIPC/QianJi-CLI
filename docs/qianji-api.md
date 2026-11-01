# 钱迹 API 分析

来源：`com.mutangtech.qianji` 4.5.4（versionCode 1234）的静态分析。所有内容都未经线上请求或真机验证。

## 官方深链（`AddBillIntentAct`）

`qianji://publicapi/addbill?type=0&money=12.5&...`

| 参数 | 说明 |
| --- | --- |
| `type` | 0 支出，1 收入，2 转账，3 信用卡还款，5 报销；其他值报错 |
| `money` | 必须大于 0 |
| `time` | `yyyy-MM-dd`、`yyyy-MM-dd HH:mm` 或 `yyyy-MM-dd HH:mm:ss`，留空取当前时间 |
| `remark` | 备注，模型上限 500 字 |
| `catename` / `bookname` | 分类名 / 账本名 |
| `catechoose` | `1` 时弹出分类选择 |
| `catetheme` | 分类选择界面主题，如 `light`、`auto` |
| `accountname` / `accountname2` | 资产账户 / 转账目标账户 |
| `fee` / `coupon` | 手续费 / 优惠 |
| `showresult` | 默认 `1` |

调用之间有 3 秒节流（`auto_task_last_time`）。另有 `qianji://publicapi/entry`，未分析。

## 私有 HTTP API

- 主机：`https://api.qianjiapp.com/`，备用 `https://qianji.xxoojoke.com/`，海外 `https://qianjiga.litangkj.com/`
- 路径：`<host><ctrl>/<act>`，表单参数，登录后带 `uid` 和 `fr`（都是用户 ID）
- 鉴权：请求头 `utoken`
- 设备和版本头：`ctrl`、`act`、`mk`、`os`、`osvs`、`devbrand`、`devname`、`devid`、`vs`、`pkg`、`vsn`、`timezoneoffset`、`clang`、`cregion`
- 签名头：`reqidv2` 和 `tok`，由 `libfabricsuffer.so` 的 native 函数 `requestId` / `encreqid` 生成（纯 MD5 拼接，见下文签名方案）
- **请求头名必须全小写**：服务器对带登录态（`utoken`）的请求大小写敏感地校验请求头，`utoken` 不能写成 `Utoken`，否则返回 `ec:8888 当前版本太旧`。免鉴权接口（`client/init` 等）不校验，容易误判成版本或签名问题。注意 Go 的 `http.Header.Set` 会把头名规范成首字母大写，需直接写 `Header[name]` 绕过。
- Content-Type 为 `application/x-www-form-urlencoded; charset=UTF-8`；传输走 HTTP/1.1（App 的 Volley HurlStack 基于 HttpURLConnection）
- 账单拉取：`syncv2/pull`，分页参数 `pageoffset`、`pagesign`，增量参数 `lasttimes`（JSON）
- 账单写入：`bill/syncall`，`v` 形如 `{<key>: {changelist: [...], dellist: [...]}}`（`key` 未确认）

## 账单模型

`user_bill` 表（greenDAO，schema 128）：`billid`、`USERID`、`TIME`、`TYPE`、`REMARK`、`MONEY`、`STATUS`、`CATEGORY_ID`、`IMAGES`、`PAYTYPE`、`updatetime`、`createtime`、`PLATFORM`、`ASSETID`、`FROMID`、`TARGETID`、`EXTRA`、`DESCINFO`、`bookid`、`USERNAME`、`FROMACT`、`TARGETACT`、`IMPORT_PACK_ID`、`BOOK_NAME`。

- `TYPE`：0 支出，1 收入，2 转账，3 还款，4 收款，5 报销，6 债务，7 借出，9 还债，10 利息支出，11 利息收入，20 退款，21 已报销，22 打包报销
- `STATUS`：0 已删除，1 正常，2 未同步
- `PLATFORM`：0 手动，2 导入，120 周期任务，121 分期，122 自动记账

## 接口清单

参数列只含代码中字面可见的字段；空白表示未提取。复杂字段多以 JSON 字符串放在 `v` 中。

| 路径 | 参数 |
| --- | --- |
| `account/bind` | access_token, pt, ptuid |
| `account/bindaccount` | account, code, pwd, uid |
| `account/bindwx` | uid, wxcode |
| `account/cancelaccount` | pwd, uid |
| `account/cancelthirdaccount` | access_token, pt, ptuid |
| `account/getverifycode` | t, v |
| `account/login` | pwd, v |
| `account/loginrecords2` | uid |
| `account/loginwx` | wxcode |
| `account/logout` | uid |
| `account/refreshtoken` | uid |
| `account/register` | v |
| `account/removelogin` | did, uid, v |
| `account/setpwd` | account, code, pwd |
| `account/unbind` | pt, uid |
| `active/home` |  |
| `active/msha` |  |
| `asset/banklist` |  |
| `asset/delete` | did, uid |
| `asset/deletecheck` | did, uid |
| `asset/finish` | did, uid, v |
| `asset/info` | did, uid |
| `asset/list` | status, uid |
| `asset/listloan` | status, t, uid |
| `asset/reorder` | uid, v |
| `asset/submit` | diffparams, extra, uid, v |
| `asset/typelist` | uid |
| `asset/visible` | did, uid |
| `assetline/init` | uid |
| `assetline/list` | did, flts, uid |
| `assetline/reset` | uid, v |
| `assetline/submit` | uid, v |
| `autorecord/starttrial` |  |
| `autorecord/status` |  |
| `baoxiao/baoxiao` | bxtime, did, images, remark, tags, uid, v |
| `baoxiao/cancelbaoxiao` | uid, v |
| `baoxiao/upgradev2` | uid |
| `bill/refund2` | did, uid, v |
| `bill/syncall` | uid, v |
| `billbatch/deletev2` | billIds, uid |
| `billbatch/modify` | billIds, uid, values |
| `book/addbycode` | uid, v |
| `book/config` | did, uid, v |
| `book/copy` | uid, v |
| `book/delete` | bookid, uid |
| `book/deletecheck` | bookid, uid |
| `book/infobycode` | uid, v |
| `book/invitecode` | bookid, uid |
| `book/list` | t, uid |
| `book/members` | bookid, uid |
| `book/quit` | bookid, uid, v |
| `book/reorder` | uid, v |
| `book/submit` | uid, v |
| `book/typelist` | uid |
| `book/visible` | bookid, uid |
| `budget/list` | bookid, flts, range, uid |
| `budget/submitv2` | bookid, changelist, flts, range, uid |
| `category/changelevel` | did, parentid, uid |
| `category/deletecheckv2` | did, uid |
| `category/deletev2` | deletebills, did, newcateid, uid |
| `category/iconlist` | uid |
| `category/listv2` | bookid, t, uid |
| `category/movesubcate` | did, parentid, uid |
| `category/reorder3` | uid, v |
| `category/submitmultiple` | bookid, parentid, t, uid, v |
| `category/submitv2` | bookid, did, icon, parentid, t, uid, v |
| `client/checkupdate` |  |
| `client/init` |  |
| `client/log` |  |
| `common/uploadtoken` |  |
| `currency/listv2` | uid |
| `data/clearbook` |  |
| `data/mergebookcheck` |  |
| `data/mergebookv3` |  |
| `import/confirmresult` | did, uid, v |
| `import/deletepack` | did, uid |
| `import/packbills` | did, page, uid |
| `import/packs` | uid |
| `import/platforms` | uid |
| `import/submitfile` | assetid, bookid, filename, pt, uid, v |
| `installment/bindbill` | billId, installmentId, uid |
| `installment/config` | did, uid, v |
| `installment/delete` | did, uid |
| `installment/list` | did, flts, page, uid |
| `installment/prepay` | uid, v |
| `installment/submit` | uid, v |
| `repeat_task/bindbill` | billId, repeatTaskId, uid |
| `repeat_task/delete` | did, uid |
| `repeat_task/holidays` | uid, years |
| `repeat_task/list` | flts, page, uid |
| `repeat_task/resetcount` | did, uid |
| `repeat_task/submit` | uid, v |
| `repeat_task/toggle` | did, uid |
| `saving/canceldeposit` | did, uid |
| `saving/delete` | deleteBills, did, uid |
| `saving/deposit` | amount, billInfo, did, remark, time, uid |
| `saving/list` | uid |
| `saving/reorder` | uid, v |
| `saving/submit` | color, currency, did, icon, mode, name, remark, rule, uid |
| `settings/headimages` |  |
| `syncv2/pull` | bookid, lasttimes, pagesign, pageoffset, uid |
| `user/getaddress` | uid |
| `user/submitaddress` | address, name, phone, uid |
| `user/update` | uid, v |
| `user/updateconfig` | uid, v |
| `usercard/delete` | did, uid |
| `usercard/listv2` | uid |
| `usercard/reorder` | uid, v |
| `usercard/submit` | uid, v |
| `vip/config` |  |
| `vip/orderinfoalipay` |  |
| `vip/payresult` |  |
| `vip/vipcode` |  |
