-- GoSuxin default branding; existing custom contact values are retained.
UPDATE gf_admin SET avatar='/resource/static/brand/logo.png?v=suxin1' WHERE username='admin' OR avatar IS NULL OR avatar='' OR avatar IN ('resource/uploads/static/avatar.png','/resource/uploads/static/avatar.png');
UPDATE gf_admin SET email='56308750@qq.com' WHERE email='504500934@qq.com';
UPDATE gf_admin SET mobile='' WHERE mobile='18988375982';
UPDATE gf_admin SET tel='' WHERE tel='18988375982';
UPDATE gf_email SET sender_email='56308750@qq.com' WHERE sender_email='504500934@qq.com';
UPDATE gf_email SET mail_title=REPLACE(mail_title,'GoFly','Suxin') WHERE mail_title LIKE '%GoFly%';
