-- 011_shift_early_exit_penalty.sql

-- Fine charged when an executor ends a shift before planned_end_at.
INSERT INTO system_settings (key, value) VALUES ('shift_early_exit_penalty', '50')
ON CONFLICT (key) DO NOTHING;
