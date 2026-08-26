package com.healthlogin.app.net;

import javax.crypto.Cipher;
import javax.crypto.spec.GCMParameterSpec;
import javax.crypto.spec.SecretKeySpec;

import java.nio.charset.StandardCharsets;
import java.util.Arrays;

/**
 * AES-256-GCM decryption for the endpoint payload. The wire/asset format is
 * base64( nonce(12) || ciphertext+tag ), matching the Go handler's encrypt().
 */
final class AesGcm {
    private static final int NONCE_LEN = 12;
    private static final int TAG_BITS = 128;

    private AesGcm() {}

    static byte[] keyFromHex(String hex) {
        int n = hex.length() / 2;
        byte[] out = new byte[n];
        for (int i = 0; i < n; i++) {
            out[i] = (byte) Integer.parseInt(hex.substring(2 * i, 2 * i + 2), 16);
        }
        return out;
    }

    /** @return decrypted UTF-8 string. @throws Exception on tamper/wrong key/bad input. */
    static String decrypt(byte[] key, String base64Blob) throws Exception {
        byte[] blob = android.util.Base64.decode(base64Blob, android.util.Base64.DEFAULT);
        if (blob.length <= NONCE_LEN) throw new IllegalArgumentException("blob too short");
        byte[] nonce = Arrays.copyOfRange(blob, 0, NONCE_LEN);
        byte[] ct = Arrays.copyOfRange(blob, NONCE_LEN, blob.length);

        Cipher c = Cipher.getInstance("AES/GCM/NoPadding");
        c.init(Cipher.DECRYPT_MODE, new SecretKeySpec(key, "AES"),
                new GCMParameterSpec(TAG_BITS, nonce));
        return new String(c.doFinal(ct), StandardCharsets.UTF_8);
    }
}
