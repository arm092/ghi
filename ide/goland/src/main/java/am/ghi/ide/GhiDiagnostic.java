package am.ghi.ide;
import java.util.regex.Pattern;
record GhiDiagnostic(String path,int line,int column,int start,int end) {
    private static final Pattern PATTERN=Pattern.compile("^(.+?\\.ghi):(\\d+)(?::(\\d+))?:");
    static GhiDiagnostic parse(String text){
        var match=PATTERN.matcher(text);
        if(!match.find())return null;
        try{
            int line=Integer.parseInt(match.group(2));
            int column=match.group(3)==null?1:Integer.parseInt(match.group(3));
            if(line<1 || column<1)return null;
            return new GhiDiagnostic(match.group(1),line-1,column-1,match.start(1),match.end()-1);
        }catch(NumberFormatException ignored){return null;}
    }
}
