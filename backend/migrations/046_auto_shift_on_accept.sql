-- 046_auto_shift_on_accept.sql

-- Автооткрытие смены, когда исполнитель берёт заказ без активной смены.
-- Включено по умолчанию; длительность — из того же списка, что и ручной старт (1/3/5),
-- по умолчанию самая короткая, чтобы открытая за исполнителя смена меньше
-- подставляла его под штраф за досрочный выход.
INSERT INTO system_settings (key, value) VALUES ('auto_shift_on_accept_enabled', '1')
ON CONFLICT (key) DO NOTHING;

INSERT INTO system_settings (key, value) VALUES ('auto_shift_duration_hours', '1')
ON CONFLICT (key) DO NOTHING;
