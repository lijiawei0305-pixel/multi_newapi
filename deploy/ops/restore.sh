#!/usr/bin/env bash
# ─────────────────────────────────────────────────────────────────────────────
# restore.sh — 从 backup.sh 产物恢复 MySQL（**服务器上运行，危险操作**）。
#
#   从 db-<ts>.sql.gz 恢复整库 new-api-test。dump 由 --databases 生成，含
#   CREATE DATABASE IF NOT EXISTS + USE + DROP TABLE，导入即覆盖现有表数据。
#
# 用法：
#   ./restore.sh /root/backups/db-20260629-023000.sql.gz
#   ASSUME_YES=1 ./restore.sh <file>     # 跳过确认（自动化/演练，谨慎）
#
# 安全：默认二次确认（键入 yes）。建议恢复前先 ./backup.sh 留一份当前态。
# 注意：Redis 状态恢复不在本脚本（缓存可重建）；如需，停 redis→替换 /data/dump.rdb→起。
# ─────────────────────────────────────────────────────────────────────────────
source "$(dirname "$0")/lib.sh"
guard_not_prod
require docker
require gzip

BACKUP="${1:-}"
[ -n "$BACKUP" ] || die "用法：$0 <db-*.sql.gz>"
[ -f "$BACKUP" ] || die "找不到备份文件：$BACKUP"
case "$BACKUP" in *.sql.gz) : ;; *) die "需 .sql.gz（backup.sh 的 DB 产物）：$BACKUP" ;; esac

log "目标栈   ：$STACK（库 $DB_NAME）"
log "恢复来源 ：$BACKUP（$(du -h "$BACKUP" | cut -f1)，mtime $(date -r "$BACKUP" '+%F %T' 2>/dev/null || true)）"
warn "此操作将用备份覆盖 $STACK 的 $DB_NAME 现有数据，且不可逆。"
confirm "确认恢复到 $STACK / $DB_NAME ?"

# 可选：恢复前自动留一份当前态（除非 NO_PRE_BACKUP=1）。
if [ "${NO_PRE_BACKUP:-0}" != "1" ]; then
  log "恢复前先备份当前态（保险）…"
  "$(dirname "$0")/backup.sh" || warn "恢复前备份失败，继续（你已确认）。"
fi

log "导入中… gunzip | mysql"
gunzip -c "$BACKUP" | dc exec -T "$MYSQL_SVC" mysql -u"$DB_USER" -p"$DB_PASS"
ok "恢复完成。建议重启 app 让连接池/缓存对齐：dc restart $APP_SVC"
log "  cd $SERVER_REPO && docker compose -p $STACK --env-file $ENV_FILE -f $COMPOSE_FILE restart $APP_SVC"
