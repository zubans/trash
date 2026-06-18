-- 002_create_tables.sql
-- Users table
CREATE TABLE IF NOT EXISTS users (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    role role_type NOT NULL,
    phone VARCHAR UNIQUE NOT NULL,
    password VARCHAR NOT NULL,
    balance NUMERIC(18,2) NOT NULL DEFAULT 0,
    status status_type NOT NULL DEFAULT 'ACTIVE',
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT now()
);

-- Customer profiles
CREATE TABLE IF NOT EXISTS customer_profiles (
    user_id UUID PRIMARY KEY,
    full_name VARCHAR NOT NULL,
    address JSONB NOT NULL,
    device_os VARCHAR,
    device_id VARCHAR,
    device_ip VARCHAR,
    last_geo VARCHAR,
    CONSTRAINT fk_customer_user FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
);

-- Executor profiles
CREATE TABLE IF NOT EXISTS executor_profiles (
    user_id UUID PRIMARY KEY,
    full_name VARCHAR NOT NULL,
    work_area_id INT,
    CONSTRAINT fk_executor_user FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
    -- Assume geozones table exists elsewhere
);

-- Orders
CREATE TABLE IF NOT EXISTS orders (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    customer_id UUID NOT NULL,
    executor_id UUID,
    volume_type volume_type NOT NULL,
    speed_tariff speed_tariff_type NOT NULL,
    status order_status_type NOT NULL DEFAULT 'SEARCHING',
    hold_amount NUMERIC(18,2) NOT NULL DEFAULT 0,
    final_amount NUMERIC(18,2) NOT NULL DEFAULT 0,
    is_downgraded BOOLEAN NOT NULL DEFAULT FALSE,
    photo_url VARCHAR,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT now(),
    deadline_at TIMESTAMP WITH TIME ZONE,
    completed_at TIMESTAMP WITH TIME ZONE,
    CONSTRAINT fk_orders_customer FOREIGN KEY (customer_id) REFERENCES users(id) ON DELETE CASCADE,
    CONSTRAINT fk_orders_executor FOREIGN KEY (executor_id) REFERENCES users(id) ON DELETE SET NULL
);

-- Shifts
CREATE TABLE IF NOT EXISTS shifts (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    executor_id UUID NOT NULL,
    duration_hours INT NOT NULL,
    started_at TIMESTAMP WITH TIME ZONE NOT NULL,
    planned_end_at TIMESTAMP WITH TIME ZONE NOT NULL,
    actual_end_at TIMESTAMP WITH TIME ZONE,
    status shift_status_type NOT NULL DEFAULT 'ACTIVE',
    CONSTRAINT fk_shifts_executor FOREIGN KEY (executor_id) REFERENCES users(id) ON DELETE CASCADE
);

-- Transactions
CREATE TABLE IF NOT EXISTS transactions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL,
    order_id UUID,
    type transaction_type NOT NULL,
    amount NUMERIC(18,2) NOT NULL,
    admin_id UUID,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT now(),
    CONSTRAINT fk_transactions_user FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE,
    CONSTRAINT fk_transactions_order FOREIGN KEY (order_id) REFERENCES orders(id) ON DELETE SET NULL,
    CONSTRAINT fk_transactions_admin FOREIGN KEY (admin_id) REFERENCES users(id) ON DELETE SET NULL
);

-- Bids
CREATE TABLE IF NOT EXISTS bids (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    order_id UUID NOT NULL,
    executor_id UUID NOT NULL,
    offered_price NUMERIC(18,2) NOT NULL,
    status bid_status_type NOT NULL DEFAULT 'PENDING',
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT now(),
    CONSTRAINT fk_bids_order FOREIGN KEY (order_id) REFERENCES orders(id) ON DELETE CASCADE,
    CONSTRAINT fk_bids_executor FOREIGN KEY (executor_id) REFERENCES users(id) ON DELETE CASCADE
);

-- Chats
CREATE TABLE IF NOT EXISTS chats (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    order_id UUID NOT NULL UNIQUE,
    is_active BOOLEAN NOT NULL DEFAULT TRUE,
    CONSTRAINT fk_chats_order FOREIGN KEY (order_id) REFERENCES orders(id) ON DELETE CASCADE
);

-- Messages
CREATE TABLE IF NOT EXISTS messages (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    chat_id UUID NOT NULL,
    sender_id UUID NOT NULL,
    text TEXT NOT NULL,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT now(),
    CONSTRAINT fk_messages_chat FOREIGN KEY (chat_id) REFERENCES chats(id) ON DELETE CASCADE,
    CONSTRAINT fk_messages_sender FOREIGN KEY (sender_id) REFERENCES users(id) ON DELETE CASCADE
);

-- System settings
CREATE TABLE IF NOT EXISTS system_settings (
    id SERIAL PRIMARY KEY,
    key VARCHAR UNIQUE NOT NULL,
    value NUMERIC(18,6) NOT NULL
);

-- Indexes for performance
CREATE INDEX idx_orders_customer ON orders(customer_id);
CREATE INDEX idx_orders_executor ON orders(executor_id);
CREATE INDEX idx_bids_executor ON bids(executor_id);
CREATE INDEX idx_messages_sender ON messages(sender_id);
