-- ぬる（agentスキーマ）の行数（表ごと。スキーマ・表が無い版では none）。backup.sh がマニフェストに書き、restore-verify.sh が復元後に突き合わせる。
SELECT coalesce(string_agg(t || '=' || (xpath('/row/c/text()', query_to_xml('select count(*) as c from agent.' || t, false, true, '')))[1]::text, ' ' ORDER BY t), 'none')
FROM unnest(ARRAY['diary', 'messages', 'notes', 'reminders', 'summaries']) AS t
WHERE to_regclass('agent.' || t) IS NOT NULL;
