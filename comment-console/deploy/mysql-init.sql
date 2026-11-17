-- 官方入口已创建正式库和 comment_console 账号，补齐独立演示库权限。
CREATE DATABASE IF NOT EXISTS xhs_comment_console_demo
    CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci;
GRANT ALL PRIVILEGES ON xhs_comment_console_demo.* TO 'comment_console'@'%';
