#!/usr/bin/env bash
# ─────────────────────────────────────────────────────────────────────────────
# restore.sh — 从 backup.sh 产物恢复 MySQL（**服务器上运行，危险操作**）。
#
#   从 db-<ts>.sql.gz 恢复整库 new-api-test。dump 由 --databases 生成，含
#   CREATE DATABASE IF NOT EXISTS + USE + DROP TABLE，导入即覆盖现有表数据。
#
# 用法：
#   ./restore.sh /root/backups/db-20260629-023000.sql.gz
#
# ⚠ 目标库 new-api-test 即【生产】（2026-07-03 单栈收敛后 newapi_test 是唯一现网）。整库
#   覆盖不可逆：期间所有充值 / 套餐激活 / 代理分润 / 消耗日志将被快照替换、且用户可能已付款。
#   故本脚本【强确认】——必须【键入栈名】（非泛泛 yes），且 **ASSUME_YES 无法跳过**
#   （restore 无自动化调用方，必须人在环）。
# 安全：恢复前【强制】留一份当前态（pre-backup）；pre-backup 失败即中止——绝不在无保险时覆盖。
#       恢复目标先做剪枝免疫的安全副本再跑 pre-backup（防 KEEP=7 剪枝恰好删掉目标本身）。
# 注意：Redis 状态恢复不在本脚本（缓存可重建）；如需，停 redis→替换 /data/dump.rdb→起。
# ─────────────────────────────────────────────────────────────────────────────
source "$(dirname "$0")/lib.sh"
guard_target
require docker
require gzip
require_db_pass          # DB 口令缺失即中止（fail-closed）——恢复导入必需

BACKUP="${1:-}"
[ -n "$BACKUP" ] || die "用法：$0 <db-*.sql.gz>"
[ -f "$BACKUP" ] || die "找不到备份文件：$BACKUP"
case "$BACKUP" in *.sql.gz) : ;; *) die "需 .sql.gz（backup.sh 的 DB 产物）：$BACKUP" ;; esac

log "目标栈   ：${STACK}（库 ${DB_NAME}）"
log "恢复来源 ：${BACKUP}（$(du -h "$BACKUP" | cut -f1)，mtime $(date -r "$BACKUP" '+%F %T' 2>/dev/null || true)）"
warn "此操作将用备份【整库覆盖】生产栈 $STACK 的 $DB_NAME，不可逆——期间充值/套餐/分润/消耗日志将全部被替换。"
confirm_typed "$STACK" "确认整库覆盖【生产】库 $STACK / $DB_NAME ?"

# 先给恢复目标做安全副本：下面的强制预备份会触发 backup.sh 的 prune_keep(KEEP=7)，
# 当目标恰为第 KEEP 新（如「恢复手上最旧快照」的灾难恢复路径）时会被挤出保留窗口而 rm 掉
# ——那可能是损坏前状态的唯一拷贝。副本名不落在剪枝 glob（db-*.sql.gz）内，故免疫剪枝；
# 同文件系统硬链接零拷贝，跨文件系统回落 cp。导入一律从副本读。
ensure_backup_dir
SAFE_SRC="$BACKUP_DIR/.restore-src-$$.sql.gz"
ln -f -- "$BACKUP" "$SAFE_SRC" 2>/dev/null || cp -a -- "$BACKUP" "$SAFE_SRC" \
  || die "无法为恢复目标建立安全副本：$BACKUP → $SAFE_SRC"
trap 'rm -f -- "$SAFE_SRC"' EXIT

# 恢复前【强制】留一份当前态（可恢复保险）。失败即中止——绝不在无保险快照时覆盖生产库。
# （不提供 NO_PRE_BACKUP 逃生：连给现态拍照都做不到，就不该覆盖它。）
log "恢复前强制备份当前态（保险）…"
"$(dirname "$0")/backup.sh" || die "恢复前备份失败——已中止，绝不在无保险快照时覆盖生产库 $DB_NAME。"

log "导入中… gunzip | mysql（从安全副本 $SAFE_SRC 读取，预备份剪枝删除原文件亦不受影响）"
gunzip -c "$SAFE_SRC" | dc exec -T "$MYSQL_SVC" mysql -u"$DB_USER" -p"$DB_PASS"
ok "恢复完成。建议重启 app 让连接池/缓存对齐：dc restart $APP_SVC"
log "  cd $SERVER_REPO && docker compose -p $STACK --env-file $ENV_FILE -f $COMPOSE_FILE restart $APP_SVC"
