#!/usr/bin/env bash
# 后端接口冒烟测试：启动临时服务，走一遍主要业务流程。
# 用法：scripts/smoke.sh（需先 go build -o bin/task_publish .）
set -u
BIN=${BIN:-./bin/task_publish}
PORT=${PORT:-18080}
T=$(mktemp -d)
DB="$T/t.db"
B="http://127.0.0.1:$PORT/api"
J='Content-Type: application/json'
FAILS=0

printf 'adminpass123\n' | "$BIN" -db "$DB" create-admin admin 管理员 >/dev/null || { echo "create-admin 失败"; exit 1; }
"$BIN" -addr "127.0.0.1:$PORT" -db "$DB" >"$T/log" 2>&1 &
PID=$!
trap 'kill $PID 2>/dev/null; wait $PID 2>/dev/null; rm -rf "$T"' EXIT
for _ in $(seq 50); do curl -s "$B/me" >/dev/null && break; sleep 0.1; done

# req <cookie名> <方法> <路径> [json]：输出 "状态码 响应体"
req() {
  local jar="$T/$1.jar" m=$2 p=$3 body=${4:-}
  if [ -n "$body" ]; then
    curl -s -b "$jar" -c "$jar" -X "$m" -H "$J" -d "$body" -w ' %{http_code}' "$B$p"
  else
    curl -s -b "$jar" -c "$jar" -X "$m" -w ' %{http_code}' "$B$p"
  fi
}
# check <说明> <期望状态码> <实际输出> [响应体需包含的字符串]
check() {
  local code=${3##* } body=${3% *}
  if [ "$code" = "$2" ] && { [ -z "${4:-}" ] || [[ "$body" == *"$4"* ]]; }; then
    echo "ok   $1"
  else
    echo "FAIL $1 —— 期望 $2 ${4:-}，实际 $code $body"; FAILS=$((FAILS + 1))
  fi
}
field() { grep -oE "\"$1\":-?[0-9]+" <<<"$2" | head -1 | cut -d: -f2; }

echo "== 账号"
check "再建管理员被拒" 1 "$(printf 'adminpass123\n' | "$BIN" -db "$DB" create-admin admin2 x >/dev/null 2>&1; echo " $?")"
check "管理员登录" 204 "$(req admin POST /auth/login '{"uid":"admin","password":"adminpass123"}')"
check "注册 mem1" 201 "$(req mem1 POST /auth/register '{"uid":"mem1","nickname":"小明","password":"password123"}')"
check "注册 mem2（同昵称）" 201 "$(req mem2 POST /auth/register '{"uid":"mem2","nickname":"小明","password":"password123"}')"
check "注册 sup1" 201 "$(req sup1 POST /auth/register '{"uid":"sup1","nickname":"老张","password":"password123"}')"
check "用户 ID 重复（大小写不敏感）" 409 "$(req x POST /auth/register '{"uid":"MEM1","nickname":"a","password":"password123"}')"
check "用户 ID 含非法字符" 400 "$(req x POST /auth/register '{"uid":"a_b","nickname":"a","password":"password123"}')"
check "不能注册管理员角色（多余字段被拒）" 400 "$(req x POST /auth/register '{"uid":"zz1","nickname":"a","password":"password123","role":"admin"}')"
check "初始积分 20" 200 "$(req mem1 GET /me)" '"points":20'
check "管理员派发开关默认关闭" 200 "$(req mem1 GET /me)" '"acceptAdminDispatch":false'

echo "== 高级用户授予"
check "普通用户不能改角色" 403 "$(req mem1 PUT /admin/users/sup1/role '{"role":"super"}')"
check "授予 sup1 高级用户" 204 "$(req admin PUT /admin/users/sup1/role '{"role":"super"}')"
check "不能修改管理员角色" 404 "$(req admin PUT /admin/users/admin/role '{"role":"member"}')"

echo "== 公共仓库"
R=$(req admin POST /public/tasks '{"description":"全体可见任务","points":9,"visibility":"all"}'); check "创建全体可见任务" 201 "$R"; TA=$(field id "$R")
R=$(req admin POST /public/tasks '{"description":"隐藏任务内容","points":7,"visibility":"privileged"}'); check "创建仅高级可见任务" 201 "$R"; TP=$(field id "$R")
R=$(req admin POST /public/tasks '{"description":"禁用任务","points":3,"enabled":false}'); check "创建禁用任务" 201 "$R"; TD=$(field id "$R")
check "积分 0 被拒" 400 "$(req admin POST /public/tasks '{"description":"x","points":0}')"
check "空描述被拒" 400 "$(req admin POST /public/tasks '{"description":"  ","points":1}')"
check "普通用户不能建公共任务" 403 "$(req mem1 POST /public/tasks '{"description":"x","points":1}')"
R=$(req mem1 GET /public/tasks); check "普通用户看到全体可见" 200 "$R" "全体可见任务"
[[ "$R" != *隐藏任务内容* && "$R" != *禁用任务* ]] && echo "ok   普通用户看不到隐藏/禁用任务" || { echo "FAIL 普通用户看到了隐藏/禁用任务"; FAILS=$((FAILS + 1)); }
R=$(req sup1 GET /public/tasks); check "高级用户看到隐藏任务" 200 "$R" "隐藏任务内容"
[[ "$R" != *禁用任务* ]] && echo "ok   高级用户看不到禁用任务" || { echo "FAIL 高级用户看到了禁用任务"; FAILS=$((FAILS + 1)); }
check "管理员看到禁用任务" 200 "$(req admin GET /public/tasks)" "禁用任务"

echo "== 主动接取"
R=$(req mem1 POST "/public/tasks/$TA/claim"); check "mem1 接取全体可见任务" 201 "$R"; A1=$(field assignmentId "$R")
check "mem1 重复接取被拒" 409 "$(req mem1 POST "/public/tasks/$TA/claim")"
check "mem2 也可接取同一任务" 201 "$(req mem2 POST "/public/tasks/$TA/claim")"
check "不能接取隐藏任务" 404 "$(req mem1 POST "/public/tasks/$TP/claim")"
check "不能接取禁用任务" 404 "$(req mem1 POST "/public/tasks/$TD/claim")"
check "高级用户不能接取" 403 "$(req sup1 POST "/public/tasks/$TA/claim")"

echo "== 随机抽取"
R=$(req mem1 POST /draw); check "抽取只返回积分" 200 "$R" '"points":7'
[[ "$R" != *隐藏任务内容* && "$R" != *\"id\"* ]] && echo "ok   抽取结果不含描述和编号" || { echo "FAIL 抽取结果泄露了任务信息"; FAILS=$((FAILS + 1)); }
check "放弃本次抽取" 204 "$(req mem1 DELETE /draw)"
check "放弃后无抽取结果" 404 "$(req mem1 POST /draw/accept)"
req mem1 POST /draw >/dev/null
R=$(req mem1 POST /draw/accept); check "接取抽到的任务" 201 "$R"; A2=$(field assignmentId "$R")
check "接取后能看到内容" 200 "$(req mem1 GET /assignments/mine)" "隐藏任务内容"
check "进行中的任务被跳过，无可抽" 404 "$(req mem1 POST /draw)"

echo "== 编辑原任务不影响已接取的快照"
check "管理员修改任务" 204 "$(req admin PUT "/public/tasks/$TA" '{"description":"改过的描述","points":9,"visibility":"all","enabled":true}')"
R=$(req mem1 GET /assignments/mine); [[ "$R" == *全体可见任务* && "$R" != *改过的描述* ]] && echo "ok   快照保持不变" || { echo "FAIL 快照被修改: $R"; FAILS=$((FAILS + 1)); }

echo "== 提交与审核"
check "提交完成情况" 204 "$(req mem1 POST "/assignments/$A1/submit" '{"submission":"做完了"}')"
check "重复提交被拒" 409 "$(req mem1 POST "/assignments/$A1/submit" '{"submission":"再交"}')"
check "他人不能审核" 404 "$(req sup1 POST "/assignments/$A1/review" '{"approve":true}')"
check "管理员待审核列表" 200 "$(req admin GET /reviews)" "做完了"
check "不通过必须填原因" 400 "$(req admin POST "/assignments/$A1/review" '{"approve":false}')"
check "审核不通过" 204 "$(req admin POST "/assignments/$A1/review" '{"approve":false,"note":"不够详细"}')"
check "退回后回到进行中" 200 "$(req mem1 GET /assignments/mine)" '"reviewNote":"不够详细"'
check "修改后重新提交" 204 "$(req mem1 POST "/assignments/$A1/submit" '{"submission":"补充了细节"}')"
check "审核通过" 204 "$(req admin POST "/assignments/$A1/review" '{"approve":true}')"
check "完成后获得 9 积分 → 29" 200 "$(req mem1 GET /me)" '"points":29'
check "完成后可再次接取" 201 "$(req mem1 POST "/public/tasks/$TA/claim")"

echo "== 放弃任务"
R=$(req mem1 POST "/assignments/$A2/abandon"); check "放弃 7 积分任务扣 3 分" 200 "$R" '"penalty":3'
check "积分 29-3=26" 200 "$(req mem1 GET /me)" '"points":26'
check "已放弃的不能再放弃" 409 "$(req mem1 POST "/assignments/$A2/abandon")"

echo "== 授权与派发"
R=$(req sup1 POST /private/tasks '{"description":"高级用户私有任务","points":5}'); check "高级用户建私有任务" 201 "$R"; PS=$(field id "$R")
check "高级用户积分 10 被拒" 400 "$(req sup1 POST /private/tasks '{"description":"x","points":10}')"
check "普通用户不能建私有任务" 403 "$(req mem1 POST /private/tasks '{"description":"x","points":1}')"
R=$(req sup1 POST "/public/tasks/$TA/copy"); check "高级用户复制公共任务" 201 "$R"; PC=$(field id "$R")
check "复制保留原积分 9" 200 "$(req sup1 GET /private/tasks)" '"points":9'
check "修改描述时保留 9 积分可以" 204 "$(req sup1 PUT "/private/tasks/$PC" '{"description":"改了副本","points":9}')"
check "改积分必须在 1-9（改成 12 被拒）" 400 "$(req sup1 PUT "/private/tasks/$PC" '{"description":"改了副本","points":12}')"
check "副本独立，原任务不变" 200 "$(req admin GET /public/tasks)" "改过的描述"
check "高级用户不能复制禁用任务" 404 "$(req sup1 POST "/public/tasks/$TD/copy")"
check "未授权时不在可派发列表" 200 "$(req sup1 GET /dispatch-targets)" '[]'
check "未授权派发失败" 200 "$(req sup1 POST "/private/tasks/$PS/dispatch" '{"uids":["mem1"]}')" "未授权"
check "按用户名搜索高级用户" 200 "$(req mem1 GET "/supers?q=%E8%80%81")" '"uid":"sup1"'
check "搜索不到普通用户" 200 "$(req mem1 GET /supers?q=mem2)" '[]'
check "授权 sup1" 204 "$(req mem1 POST /authorizations '{"uid":"sup1"}')"
check "不能授权普通用户" 404 "$(req mem1 POST /authorizations '{"uid":"mem2"}')"
R=$(req sup1 POST "/private/tasks/$PS/dispatch" '{"uids":["mem1","mem2","nobody"]}')
check "派发：mem1 成功" 200 "$R" '{"uid":"mem1","ok":true}'
check "派发：mem2 未授权" 200 "$R" '"uid":"mem2","ok":false'
check "派发：用户不存在" 200 "$R" '"uid":"nobody","ok":false'
check "重复派发同一任务被拒" 200 "$(req sup1 POST "/private/tasks/$PS/dispatch" '{"uids":["mem1"]}')" "完成前不能重复接取"
R=$(req admin POST /private/tasks '{"description":"管理员私有任务","points":100}'); check "管理员私有任务积分可超过 9" 201 "$R"; PA=$(field id "$R")
check "管理员派发：开关未开" 200 "$(req admin POST "/private/tasks/$PA/dispatch" '{"uids":["mem1"]}')" "未开启"
check "打开接受管理员派发" 204 "$(req mem1 PUT /me/settings '{"acceptAdminDispatch":true}')"
check "管理员派发成功" 200 "$(req admin POST "/private/tasks/$PA/dispatch" '{"uids":["mem1"]}')" '"ok":true'
check "管理员不能派发公共任务" 404 "$(req admin POST "/private/tasks/$TA/dispatch" '{"uids":["mem1"]}')"

echo "== 撤销高级用户"
AD=$(req mem1 GET /assignments/mine | grep -oE '"id":[0-9]+,"taskId":'"$PS"',' | head -1 | cut -d, -f1 | cut -d: -f2)
check "mem1 提交派发的任务" 204 "$(req mem1 POST "/assignments/$AD/submit" '{"submission":"派发任务完成"}')"
check "撤销 sup1" 204 "$(req admin PUT /admin/users/sup1/role '{"role":"member"}')"
check "私有仓库冻结但可查看" 200 "$(req sup1 GET /private/tasks)" '"frozen":true'
check "冻结后不能建任务" 403 "$(req sup1 POST /private/tasks '{"description":"x","points":1}')"
check "冻结后不能派发" 403 "$(req sup1 POST "/private/tasks/$PS/dispatch" '{"uids":["mem1"]}')"
check "授权仍保留，标记为失效" 200 "$(req mem1 GET /authorizations)" '"active":false'
check "仍可审核已派发的任务" 204 "$(req sup1 POST "/assignments/$AD/review" '{"approve":true}')"
check "mem1 获得 5 积分 → 31" 200 "$(req mem1 GET /me)" '"points":31'
check "被撤销后可接取任务" 201 "$(req sup1 POST "/public/tasks/$TA/claim")"
check "重新授予" 204 "$(req admin PUT /admin/users/sup1/role '{"role":"super"}')"
check "私有仓库恢复" 200 "$(req sup1 GET /private/tasks)" '"frozen":false'

echo "== 负积分不能放弃"
R=$(req admin POST /public/tasks '{"description":"高分任务","points":200}'); TB=$(field id "$R")
R=$(req mem2 POST "/public/tasks/$TB/claim"); AB=$(field assignmentId "$R")
check "mem2 放弃 200 积分任务，20-100=-80" 200 "$(req mem2 POST "/assignments/$AB/abandon")" '"points":-80'
R=$(req mem2 POST "/public/tasks/$TB/claim"); AB2=$(field assignmentId "$R")
check "积分为负时不能放弃" 409 "$(req mem2 POST "/assignments/$AB2/abandon")"

echo "== 删除原任务后接取记录保留"
check "删除公共任务" 204 "$(req admin DELETE "/public/tasks/$TB")"
check "接取记录仍在" 200 "$(req mem2 GET /assignments/mine)" '"taskId":'"$TB"

echo "== 个人设置"
check "修改用户名" 204 "$(req mem1 PUT /me/profile '{"nickname":"  新名字  "}')"
check "用户名已更新（去除首尾空格）" 200 "$(req mem1 GET /me)" '"nickname":"新名字"'
check "空用户名被拒" 400 "$(req mem1 PUT /me/profile '{"nickname":"   "}')"
check "管理员也能改用户名" 204 "$(req admin PUT /me/profile '{"nickname":"超级管理员"}')"
check "另一设备登录 mem1" 204 "$(req mem1c POST /auth/login '{"uid":"mem1","password":"password123"}')"
check "原密码错误被拒" 400 "$(req mem1 PUT /me/password '{"oldPassword":"wrongpass1","newPassword":"newpassword1"}')"
check "新密码过短被拒" 400 "$(req mem1 PUT /me/password '{"oldPassword":"password123","newPassword":"short"}')"
check "修改密码" 204 "$(req mem1 PUT /me/password '{"oldPassword":"password123","newPassword":"newpassword1"}')"
check "当前会话仍有效" 200 "$(req mem1 GET /me)"
check "其他设备会话失效" 401 "$(req mem1c GET /me)"
check "旧密码不能登录" 401 "$(req x POST /auth/login '{"uid":"mem1","password":"password123"}')"
check "新密码可以登录" 204 "$(req x POST /auth/login '{"uid":"mem1","password":"newpassword1"}')"

echo "== 其他"
check "退出" 204 "$(req mem1 POST /auth/logout)"
check "退出后未登录" 401 "$(req mem1 GET /me)"
check "错误的 Content-Type" 415 "$(curl -s -b "$T/admin.jar" -d '{}' -w ' %{http_code}' "$B/public/tasks")"

echo
if [ "$FAILS" -eq 0 ]; then echo "全部通过"; else echo "$FAILS 项失败"; grep -i error "$T/log" | tail -5; exit 1; fi
