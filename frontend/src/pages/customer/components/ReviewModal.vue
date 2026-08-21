<template>
  <div v-if="show" class="modal-overlay" @click.self="show = false">
    <div class="review-modal-card">
      <div class="review-header">
        <button type="button" class="btn-close" aria-label="Закрыть" @click="show = false">
          <i class="ph ph-x"></i>
        </button>
        <div class="overline">Оценка выполнения</div>
        <h3 class="modal-title">Как всё прошло?</h3>
        <p class="modal-subtitle">Оцените работу участника по заказу #{{ orderId.slice(0, 8) }}</p>
      </div>

      <div class="review-body">
        <!-- Interactive Star Rating -->
        <div class="rating-stars-wrapper">
          <button
            v-for="star in 5"
            :key="star"
            type="button"
            :class="['star-btn', { active: star <= (hoverRating || selectedRating) }]"
            @mouseenter="hoverRating = star"
            @mouseleave="hoverRating = 0"
            @click="selectedRating = star"
          >
            <i :class="[star <= (hoverRating || selectedRating) ? 'ph-fill ph-star' : 'ph ph-star']"></i>
          </button>
        </div>

        <div v-if="selectedRating > 0" class="rating-label">
          {{ ratingLabels[selectedRating] }}
        </div>

        <!-- Quick Tags -->
        <div class="quick-tags-section">
          <div class="tags-label">Что вы хотите отметить?</div>
          <div class="tags-grid">
            <button
              v-for="tag in availableTags"
              :key="tag"
              type="button"
              :class="['tag-pill', { active: selectedTags.includes(tag) }]"
              @click="toggleTag(tag)"
            >
              {{ tag }}
            </button>
          </div>
        </div>

        <!-- Comment Field -->
        <div class="comment-field-group">
          <label class="comment-label">Отзыв (необязательно)</label>
          <textarea
            v-model="comment"
            class="comment-textarea"
            placeholder="Поделитесь подробностями выполнения..."
            rows="3"
            maxlength="500"
          ></textarea>
        </div>

        <div v-if="errorText" class="error-banner">
          {{ errorText }}
        </div>
      </div>

      <div class="review-footer">
        <button
          type="button"
          class="btn-submit-review"
          :disabled="selectedRating === 0 || submitting"
          @click="submit"
        >
          <span v-if="submitting" class="spinner-sm"></span>
          <template v-else>
            Отправить отзыв <i class="ph-bold ph-paper-plane-tilt"></i>
          </template>
        </button>
      </div>
    </div>
  </div>
</template>

<script lang="ts">
import { defineComponent, ref, computed, watch } from 'vue'
import { submitOrderReview } from '../../../api/review'

export default defineComponent({
  name: 'ReviewModal',
  props: {
    modelValue: { type: Boolean, required: true },
    orderId: { type: String, required: true },
    role: { type: String, default: 'CUSTOMER' },
  },
  emits: ['update:modelValue', 'reviewed'],
  setup(props, { emit }) {
    const show = computed({
      get: () => props.modelValue,
      set: (val) => emit('update:modelValue', val),
    })

    // Overflow handled by parent views

    const selectedRating = ref(5)
    const hoverRating = ref(0)
    const selectedTags = ref<string[]>([])
    const comment = ref('')
    const submitting = ref(false)
    const errorText = ref('')

    const ratingLabels: Record<number, string> = {
      1: 'Ужасно',
      2: 'Плохо',
      3: 'Удовлетворительно',
      4: 'Хорошо',
      5: 'Отлично!',
    }

    const availableTags = computed(() => {
      if (props.role === 'CUSTOMER') {
        return ['Пунктуален', 'Быстро', 'Аккуратно', 'Вежливый', 'Качественно']
      }
      return ['Точный адрес', 'Мусор собран', 'Быстрое подтверждение', 'Вежливый']
    })

    const toggleTag = (tag: string) => {
      const idx = selectedTags.value.indexOf(tag)
      if (idx >= 0) {
        selectedTags.value.splice(idx, 1)
      } else {
        selectedTags.value.push(tag)
      }
    }

    const submit = async () => {
      if (selectedRating.value === 0 || submitting.value) return
      submitting.value = true
      errorText.value = ''

      try {
        await submitOrderReview(props.orderId, {
          rating: selectedRating.value,
          tags: selectedTags.value,
          comment: comment.value.trim(),
        })
        emit('reviewed')
        show.value = false
      } catch (err: any) {
        errorText.value = err.response?.data || 'Ошибка отправки отзыва'
      } finally {
        submitting.value = false
      }
    }

    return {
      show,
      selectedRating,
      hoverRating,
      selectedTags,
      comment,
      submitting,
      errorText,
      ratingLabels,
      availableTags,
      toggleTag,
      submit,
    }
  },
})
</script>

<style scoped>
.modal-overlay {
  position: fixed;
  top: 0; left: 0; right: 0; bottom: 0;
  background: rgba(15, 23, 42, 0.7);
  backdrop-filter: blur(12px);
  -webkit-backdrop-filter: blur(12px);
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 24px;
  z-index: 9999;
}

.review-modal-card {
  background: #ffffff;
  border-radius: 28px;
  width: 100%;
  max-width: 440px;
  padding: 28px;
  box-shadow: 0 25px 50px -12px rgba(0, 0, 0, 0.25);
  position: relative;
  animation: modalSlideUp 0.3s cubic-bezier(0.16, 1, 0.3, 1);
}

@keyframes modalSlideUp {
  from { opacity: 0; transform: translateY(20px); }
  to { opacity: 1; transform: translateY(0); }
}

.btn-close {
  position: absolute;
  top: 24px; right: 24px;
  width: 36px; height: 36px;
  border-radius: 50%; border: none;
  background: #f1f5f9; color: #64748b;
  font-size: 18px; cursor: pointer;
  display: flex; align-items: center; justify-content: center;
  transition: all 0.2s ease;
}
.btn-close:hover { background: #e2e8f0; color: #0f172a; }

.overline { font-size: 12px; font-weight: 700; color: #6366f1; text-transform: uppercase; letter-spacing: 0.5px; margin-bottom: 4px; }
.modal-title { font-size: 22px; font-weight: 700; color: #0f172a; margin: 0 0 4px 0; }
.modal-subtitle { font-size: 14px; color: #64748b; margin: 0 0 24px 0; }

.rating-stars-wrapper {
  display: flex;
  justify-content: center;
  gap: 12px;
  margin-bottom: 12px;
}

.star-btn {
  background: transparent;
  border: none;
  font-size: 36px;
  color: #cbd5e1;
  cursor: pointer;
  transition: transform 0.15s ease, color 0.15s ease;
}

.star-btn.active {
  color: #f59e0b;
}

.star-btn:hover {
  transform: scale(1.15);
}

.rating-label {
  text-align: center;
  font-size: 16px;
  font-weight: 700;
  color: #0f172a;
  margin-bottom: 20px;
}

.quick-tags-section {
  margin-bottom: 20px;
}

.tags-label {
  font-size: 13px;
  font-weight: 600;
  color: #475569;
  margin-bottom: 10px;
}

.tags-grid {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
}

.tag-pill {
  padding: 8px 14px;
  border-radius: 99px;
  border: 1px solid #e2e8f0;
  background: #f8fafc;
  color: #475569;
  font-size: 13px;
  font-weight: 500;
  cursor: pointer;
  transition: all 0.2s ease;
}

.tag-pill:hover {
  border-color: #cbd5e1;
  background: #f1f5f9;
}

.tag-pill.active {
  background: #eef2ff;
  border-color: #6366f1;
  color: #4f46e5;
  font-weight: 600;
}

.comment-field-group {
  margin-bottom: 20px;
}

.comment-label {
  display: block;
  font-size: 13px;
  font-weight: 600;
  color: #475569;
  margin-bottom: 6px;
}

.comment-textarea {
  width: 100%;
  padding: 12px 14px;
  border-radius: 14px;
  border: 1px solid #e2e8f0;
  background: #f8fafc;
  font-family: inherit;
  font-size: 14px;
  color: #0f172a;
  outline: none;
  resize: none;
  transition: border-color 0.2s ease;
}

.comment-textarea:focus {
  border-color: #6366f1;
  background: #ffffff;
}

.error-banner {
  background: #fef2f2;
  border: 1px solid #fecaca;
  color: #ef4444;
  padding: 10px 14px;
  border-radius: 12px;
  font-size: 13px;
  margin-bottom: 16px;
}

.btn-submit-review {
  width: 100%;
  padding: 14px;
  border-radius: 16px;
  border: none;
  background: linear-gradient(135deg, #6366f1, #4f46e5);
  color: #ffffff;
  font-size: 16px;
  font-weight: 600;
  cursor: pointer;
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 8px;
  box-shadow: 0 10px 20px -5px rgba(99, 102, 241, 0.4);
  transition: all 0.2s ease;
}

.btn-submit-review:hover {
  transform: translateY(-1px);
  box-shadow: 0 14px 24px -5px rgba(99, 102, 241, 0.5);
}

.btn-submit-review:disabled {
  opacity: 0.5;
  cursor: not-allowed;
  transform: none;
}
</style>
