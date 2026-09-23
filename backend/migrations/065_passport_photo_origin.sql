-- 065_passport_photo_origin.sql
-- Откуда взялось фото паспорта: снято в приложении или принесено готовым.
--
-- Фото документа принимается только живой съёмкой, но запретить это на клиенте
-- нельзя — браузер всегда даст выбрать файл. Поэтому приложение подписывает
-- снимок ключом, который получает перед съёмкой, и вкладывает в изображение
-- незаметную метку — тем же кодом, что фото-подтверждение заказов
-- (backend/photoproof). Сервер проверяет и то и другое и хранит вердикт.
--
-- Проверка скрытая: человеку она ничего не говорит и загрузку не отклоняет.
-- Вердикт видит модератор, который решает, ставить ли «проверенного»: снимок
-- без подписи — повод посмотреть внимательнее, а не отказ.
--
-- photo_taken_at — время съёмки по часам телефона. Оно входит в подпись, и без
-- него проверить её нельзя.

ALTER TABLE user_passports
    ADD COLUMN IF NOT EXISTS photo_seal VARCHAR(8) NULL,
    ADD COLUMN IF NOT EXISTS photo_mark VARCHAR(12) NULL,
    ADD COLUMN IF NOT EXISTS photo_taken_at TIMESTAMPTZ NULL;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint WHERE conname = 'user_passports_photo_seal_check'
    ) THEN
        ALTER TABLE user_passports
            ADD CONSTRAINT user_passports_photo_seal_check
            CHECK (photo_seal IS NULL OR photo_seal IN ('VALID', 'MISSING', 'INVALID'));
    END IF;
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint WHERE conname = 'user_passports_photo_mark_check'
    ) THEN
        ALTER TABLE user_passports
            ADD CONSTRAINT user_passports_photo_mark_check
            CHECK (photo_mark IS NULL OR photo_mark IN ('FOUND', 'NOT_FOUND', 'MISMATCH'));
    END IF;
END $$;
