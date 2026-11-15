DROP TABLE IF EXISTS document_block_activity;
DROP TABLE IF EXISTS document_updates;
DROP TABLE IF EXISTS document_snapshots;
DROP TABLE IF EXISTS documents;
DROP TABLE IF EXISTS notebooks;

DELETE FROM role_permissions WHERE permission_id IN (SELECT id FROM permissions WHERE code LIKE 'doc.%');
DELETE FROM permissions WHERE code LIKE 'doc.%';
