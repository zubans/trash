# Здесь добавляются правила ProGuard, специфичные для проекта.
# Набор применяемых конфигурационных файлов задаётся настройкой
# proguardFiles в build.gradle.
#
# Подробности см.
#   http://developer.android.com/guide/developing/tools/proguard.html

# Если проект использует WebView с JS, раскомментируйте следующее
# и укажите полное имя класса JavaScript-интерфейса:
#-keepclassmembers class fqcn.of.javascript.interface.for.webview {
#   public *;
#}

# Раскомментируйте, чтобы сохранить сведения о номерах строк
# для отладки стектрейсов.
#-keepattributes SourceFile,LineNumberTable

# Если сведения о номерах строк сохраняются, раскомментируйте это,
# чтобы скрыть исходное имя файла.
#-renamesourcefileattribute SourceFile
