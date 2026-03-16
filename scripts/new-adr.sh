#!/usr/bin/env bash
#
# Создать новый ADR файл по шаблону.
# Использование: ./scripts/new-adr.sh "название решения"
# Пример:       ./scripts/new-adr.sh "использовать postgresql для очереди"
#

set -e

DECISIONS_DIR="docs/decisions"
TEMPLATE="$DECISIONS_DIR/ADR-000-template.md"

if [ -z "$1" ]; then
  echo "Использование: $0 \"название решения\""
  echo "Пример:       $0 \"использовать redis для кэша\""
  exit 1
fi

# Найти следующий номер
LAST=$(ls "$DECISIONS_DIR"/ADR-[0-9]*.md 2>/dev/null | grep -v ADR-000 | sort | tail -1)
if [ -z "$LAST" ]; then
  NEXT_NUM=1
else
  LAST_NUM=$(basename "$LAST" | grep -oE '^ADR-([0-9]+)' | grep -oE '[0-9]+')
  NEXT_NUM=$((10#$LAST_NUM + 1))
fi

NEXT=$(printf "%03d" "$NEXT_NUM")

# Преобразовать название в slug (строчные, пробелы → дефисы)
SLUG=$(echo "$1" | tr '[:upper:]' '[:lower:]' | tr ' ' '-' | tr -cd '[:alnum:]-')

FILENAME="$DECISIONS_DIR/ADR-${NEXT}-${SLUG}.md"
DATE=$(date +%Y-%m-%d)

# Создать из шаблона
cp "$TEMPLATE" "$FILENAME"

# Подставить номер и дату
sed -i '' "s/ADR-NNN/ADR-${NEXT}/g" "$FILENAME"
sed -i '' "s/YYYY-MM-DD/$DATE/g" "$FILENAME"

echo ""
echo "✓ Создан: $FILENAME"
echo ""
echo "Следующие шаги:"
echo "  1. Заполните разделы: Контекст, Варианты, Решение, Последствия"
echo "  2. Обновите docs/decisions/README.md — добавьте строку в таблицу:"
echo "     | [ADR-${NEXT}](ADR-${NEXT}-${SLUG}.md) | $1 | proposed |"
echo "  3. Обновите CHANGELOG.md — добавьте запись в [Unreleased]"
echo ""
